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
  "{\"name\":\"Verify Harness\",\"email\":\"dispute-customer-$$@example.com\",\"phone\":\"04190$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/dispute-customer.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/dispute-customer.json"; fail "could not register the dispute customer: $status"; }
dispute_customer_id="$(json "$WORKDIR/dispute-customer.json" '["id"]')"
dispute_customer_token="$(mint_token "$dispute_customer_id")"

status="$(post_json "verify-adm-prov-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"dispute-provider-$$@example.com\",\"phone\":\"04191$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/dispute-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/dispute-provider.json"; fail "could not register the dispute provider: $status"; }
dispute_provider_id="$(json "$WORKDIR/dispute-provider.json" '["id"]')"
dispute_provider_token="$(mint_token "$dispute_provider_id")"

status="$(post_json "verify-adm-other-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"dispute-other-$$@example.com\",\"phone\":\"04192$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
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

# dispute_move_because <job> <from> <to> <actor-kind> <reason> — a guarded transition that records
# a reason and names which kind of actor made it (SHIP-158).
#
# **It takes the row it wrote through RETURNING rather than re-querying on (job_id, to_status)**,
# which dispute_move above does and which is ambiguous the moment a job reaches one status twice —
# Draft to Open, then Awarded back to Open, which is exactly the shape this ticket is about. The
# guard would be handed whichever row the planner returned first.
dispute_move_because() {
  local actor_id="$dispute_customer_id"
  [[ "$4" == "provider" ]] && actor_id="$dispute_provider_id"

  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 \
    -v job="$1" -v from_status="$2" -v to_status="$3" -v actor_type="$4" -v reason="$5" \
    -v actor="$actor_id" >/dev/null <<'SQL'
BEGIN;
WITH written AS (
    INSERT INTO job_status_history
        (id, job_id, from_status, to_status, actor_type, actor_id, reason, actor_recorded_at)
    VALUES (gen_random_uuid(), :'job', :'from_status', :'to_status', :'actor_type', :'actor',
            :'reason', now())
    RETURNING id
)
SELECT set_config('shipper.job_status_transition', (SELECT id::text FROM written), true);
UPDATE jobs SET status = :'to_status' WHERE id = :'job';
COMMIT;
SQL
}

# dispute_award <job-id> <provider-id> — the accepted bid SHIP-92 will write.
dispute_award() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -v job="$1" -v provider="$2" >/dev/null <<'SQL'
INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
VALUES (gen_random_uuid(), :'job', :'provider', 'Accepted', 450.00,
        now() + interval '2 days', now() + interval '3 days');
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

# dispute_awarded_job <label> — a job committed to a provider and no further (SHIP-157).
#
# The first status at which a job can be *overdue for its pickup*: before award nobody has undertaken
# to collect it, and an Open job past its pickup window is SHIP-68's expiry sweep rather than a
# delivery exception.
dispute_awarded_job() {
  local job
  job="$(dispute_draft "$1")"
  dispute_move "$job" Draft Open
  dispute_move "$job" Open Awarded
  dispute_award "$job" "$dispute_provider_id"
  printf '%s' "$job"
}

# dispute_transit_job <label> — a job collected and moving, not yet delivered (SHIP-157).
#
# The status a *delayed delivery* is measured at: the goods are with the provider and have not
# arrived. Built to this status rather than moved back from Delivered, because Docs/02 §2 offers no
# transition backwards and the guard would refuse one.
dispute_transit_job() {
  local job
  job="$(dispute_draft "$1")"
  dispute_move "$job" Draft Open
  dispute_move "$job" Open Awarded
  dispute_award "$job" "$dispute_provider_id"
  dispute_move "$job" Awarded 'En route to pickup'
  dispute_move "$job" 'En route to pickup' 'Picked up'
  dispute_move "$job" 'Picked up' 'In transit'
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

# ==========================================================================================
# SHIP-147 — administrator sign-in, and the two directions it must not be reachable in.
#
# # This section rate-limits, and it clears its own keys at both ends
#
# `make verify` runs every request from 127.0.0.1, so the per-address bucket administrator sign-in
# uses is shared with every other section and with every concurrent worktree. The keys are
# `rl:v1:admin-signin:account:*` and `rl:v1:admin-credential:address:*` — a different namespace from
# identity's, because an administrator's failed attempts and a user's are separate allowances — and
# they are deleted before the first sign-in and after the last, exactly as 40-identity.sh does with
# its own. The address half carries the class's name rather than sign-in's since SHIP-183b, which is
# where identity's stopped being sign-in's alone.
#
# # The bootstrap administrator is inserted with SQL, and there is no endpoint that would do it
#
# The first administrator in any deployment cannot come from an authenticated administrator
# endpoint, so it comes from an INSERT. The hash below is a **development fixture** in the same
# sense as the signing key `mint_token` uses: it is committed on purpose, it hashes a password that
# exists only in this file, and it is worth nothing against any database that was not seeded with
# it. It is written at a reduced argon2id cost, which the service silently upgrades to the
# configured profile at the first successful sign-in (SHIP-29) — a path this section exercises
# without asserting on, because that upgrade is `internal/passwords`' to prove.
#
# # The fixture prefix is 0419 and no account below needs one
#
# Nothing here registers a marketplace user: an administrator has no `users` row, which is the whole
# of the ticket. The one mobile token used below is minted by the harness for a subject that does
# not exist, which is exactly right — the point being made is that the token is refused whatever it
# claims.

ticket "SHIP-147  administrator sign-in is a separate system, and a user token cannot reach it"

# Two patterns, since SHIP-183b renamed the address half to `admin-credential:address:` to match
# the class it belongs to. The account half is still `admin-signin:account:`. Clearing only the
# first would report a clean namespace and leave the address bucket full for the next section.
admin_limit_keys() {
  redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:admin-signin:*'
  redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:admin-credential:*'
}

admin_clear_limits() {
  admin_limit_keys | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true
}
admin_clear_limits

# A development fixture, not a secret. See the section header.
admin_password="verify-admin-fixture-password"
admin_fixture_hash='$argon2id$v=19$m=8192,t=1,p=1$aV5lLp4R4eGpoUzF2u+TDw$F2Vdnh8pmtZWQShAmx1p0L2X4qWMvmSFdnqSNhOJWZA'

admin_email="verify-admin-$$@example.com"
admin_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$admin_email', 'Verify Owner', '$admin_fixture_hash', 'owner')
   returning id;")"
[[ -n "$admin_id" ]] || fail "the bootstrap administrator could not be created"

# admin_signin <key> <email> <password> <name> — one sign-in, answering with its status.
admin_signin() {
  post_json "$1" /v1/admin/sessions \
    "{\"email\":\"$2\",\"password\":\"$3\"}" "$WORKDIR/admin-$4.json"
}

# admin_post <path> <credential> <key> <body> <name> — one authenticated state-changing request
# (SHIP-166).
#
# A general helper rather than a fourth endpoint-specific one like `unpublish` below: the two
# suspension routes and every administrative POST after them take the same four things, and one more
# near-identical function per route is how a harness ends up with six that differ by a path.
admin_post() {
  curl -s -X POST -o "$WORKDIR/admin-$5.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $2" -H "Idempotency-Key: $3" \
    -H 'Content-Type: application/json' -d "$4" \
    "http://localhost:$VERIFY_PORT$1"
}

# admin_get <path> <credential> <name> — one authenticated read, answering with its status.
#
# The credential is passed verbatim so that the two crossing checks below can present the *other*
# system's token through exactly the same call.
admin_get() {
  curl -s -o "$WORKDIR/admin-$3.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $2" "http://localhost:$VERIFY_PORT$1"
}

status="$(admin_signin "verify-adm-signin-$$" "$admin_email" "$admin_password" signin)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-signin.json"; fail "administrator sign-in returned $status, want 200"; }

admin_token="$(json "$WORKDIR/admin-signin.json" '["token"]')"
[[ -n "$admin_token" ]] || fail "sign-in returned no token"
[[ "$(json "$WORKDIR/admin-signin.json" '["administrator"]["role"]')" == "owner" ]] \
  || { cat "$WORKDIR/admin-signin.json"; fail "the administrator's role is not what was stored"; }
[[ -n "$(json "$WORKDIR/admin-signin.json" '["session"]["expires_at"]')" ]] \
  || fail "sign-in returned no expiry, so a console cannot tell when its credential dies"
ok "an administrator signs in and is handed a session with a lifetime"

# The credential is a digest in the table and the token is nowhere in it. A readable one would make
# every backup and every replica a way into the console.
stored_admin_hash="$("$PSQL" "$DATABASE_URL" -tAc \
  "select token_hash from admin_sessions where admin_user_id = '$admin_id';")"
[[ -n "$stored_admin_hash" ]] || fail "no session row was written"
[[ "$stored_admin_hash" != "$admin_token" ]] || fail "the session token is stored verbatim"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from admin_sessions where token_hash like '%$admin_token%';")" == "0" ]] \
  || fail "the session token appears in admin_sessions"
ok "what is stored is a digest, and the token itself is not in the table"

status="$(admin_get /v1/admin/me "$admin_token" me)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-me.json"; fail "GET /v1/admin/me returned $status, want 200"; }
[[ "$(json "$WORKDIR/admin-me.json" '["administrator"]["id"]')" == "$admin_id" ]] \
  || { cat "$WORKDIR/admin-me.json"; fail "the session resolved to the wrong administrator"; }
ok "the credential resolves on a RequireAdmin route, which is the guard SHIP-15r left unfilled"

# --- the *Done when*'s second clause, on the wire, in both directions ----------------------------
#
# One of these is the sentence the ticket is written around. The other is the direction a plausible
# implementation leaves open, and it is the one wave 7 found a real defect in on the driver pair.

crossing_user_token="$(mint_token "$(uuidgen | tr 'A-Z' 'a-z')")"

status="$(admin_get /v1/admin/me "$crossing_user_token" me-with-user-token)"
[[ "$status" == "401" ]] \
  || { cat "$WORKDIR/admin-me-with-user-token.json"; fail "a mobile access token reached an administrator route with $status"; }
ok "a mobile access token does not reach an administrator route"

status="$(admin_get /v1/jobs "$admin_token" jobs-with-admin-token)"
[[ "$status" == "401" ]] \
  || { cat "$WORKDIR/admin-jobs-with-admin-token.json"; fail "an administrator credential reached a mobile route with $status"; }
ok "and an administrator credential does not reach a mobile route — neither can be exchanged for the other"

status="$(admin_get /v1/admin/me "" me-no-credential)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/admin-me-no-credential.json"; fail "an administrator route with no credential returned $status"; }
ok "and presenting nothing is refused rather than served"

# --- one answer for two failures, and nothing said about which ----------------------------------

status="$(admin_signin "verify-adm-wrong-$$" "$admin_email" "not-the-password" wrong)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/admin-wrong.json"; fail "a wrong administrator password returned $status, want 400"; }
wrong_code="$(json "$WORKDIR/admin-wrong.json" '["error"]["code"]')"

status="$(admin_signin "verify-adm-nobody-$$" "verify-nobody-$$@example.com" "$admin_password" nobody)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/admin-nobody.json"; fail "an unknown administrator address returned $status, want 400"; }
unknown_code="$(json "$WORKDIR/admin-nobody.json" '["error"]["code"]')"

[[ "$wrong_code" == "admin_credentials_invalid" && "$unknown_code" == "admin_credentials_invalid" ]] \
  || fail "a wrong password answers $wrong_code and an unknown address answers $unknown_code, so sign-in says which addresses are administrators"
ok "a wrong password and an unknown address are one answer, so sign-in is not an administrator-address oracle"

# --- signing out ends the session at once, and says so twice ------------------------------------

status="$(curl -s -X DELETE -o "$WORKDIR/admin-signout.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $admin_token" -H "Idempotency-Key: verify-adm-signout-$$" \
  "http://localhost:$VERIFY_PORT/v1/admin/sessions/current")"
[[ "$status" == "204" ]] || { cat "$WORKDIR/admin-signout.json"; fail "signing out returned $status, want 204"; }

status="$(admin_get /v1/admin/me "$admin_token" me-after-signout)"
[[ "$status" == "401" ]] \
  || { cat "$WORKDIR/admin-me-after-signout.json"; fail "a signed-out credential still works ($status)"; }
ok "signing out ends the session immediately — the credential is a row, so there is no window"

# A browser that retried the same request reuses its key, and the middleware replays the 204 it
# already sent — from outside the guard, so the dead credential is never consulted. That is the
# retry path this endpoint has to survive, and it is a different thing from the domain's own
# idempotence.
status="$(curl -s -X DELETE -o "$WORKDIR/admin-signout-replay.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $admin_token" -H "Idempotency-Key: verify-adm-signout-$$" \
  "http://localhost:$VERIFY_PORT/v1/admin/sessions/current")"
[[ "$status" == "204" ]] \
  || { cat "$WORKDIR/admin-signout-replay.json"; fail "a retried sign-out returned $status, want the replayed 204"; }
ok "a retry with the same key is replayed, so a dropped connection does not report a failed sign-out"

# A *fresh* key reaches the guard, which refuses the credential the first call ended.
status="$(curl -s -X DELETE -o "$WORKDIR/admin-signout-again.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $admin_token" -H "Idempotency-Key: verify-adm-signout2-$$" \
  "http://localhost:$VERIFY_PORT/v1/admin/sessions/current")"
[[ "$status" == "401" ]] \
  || { cat "$WORKDIR/admin-signout-again.json"; fail "a second sign-out with a dead credential returned $status, want 401"; }
ok "and a fresh request with the credential it ended is refused by the guard"

# --- a disabled account loses its live session, and is told why when it signs in ------------------

disabled_email="verify-admin-disabled-$$@example.com"
disabled_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$disabled_email', 'Verify Leaver', '$admin_fixture_hash', 'support')
   returning id;")"
[[ -n "$disabled_id" ]] || fail "the second administrator could not be created"

status="$(admin_signin "verify-adm-disabled-in-$$" "$disabled_email" "$admin_password" disabled-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-disabled-in.json"; fail "the second administrator could not sign in ($status)"; }
disabled_token="$(json "$WORKDIR/admin-disabled-in.json" '["token"]')"

"$PSQL" "$DATABASE_URL" -q -c \
  "update admin_users set status = 'disabled' where id = '$disabled_id';" >/dev/null \
  || fail "the account could not be disabled"

status="$(admin_get /v1/admin/me "$disabled_token" me-disabled)"
[[ "$status" == "401" ]] \
  || { cat "$WORKDIR/admin-me-disabled.json"; fail "a disabled administrator's live session still works ($status)"; }
ok "disabling an account ends its live sessions at once, with no second write and no waiting for an expiry"

status="$(admin_signin "verify-adm-disabled-again-$$" "$disabled_email" "$admin_password" disabled-again)"
[[ "$status" == "403" ]] || { cat "$WORKDIR/admin-disabled-again.json"; fail "a disabled account signing in returned $status, want 403"; }
[[ "$(json "$WORKDIR/admin-disabled-again.json" '["error"]["code"]')" == "admin_account_disabled" ]] \
  || { cat "$WORKDIR/admin-disabled-again.json"; fail "a disabled account is not told why"; }
ok "and a fresh sign-in is told why, which is safe only because the password was proved first"

# --- the idempotency middleware is in front of this endpoint like every other -------------------

status="$(curl -s -X POST -o "$WORKDIR/admin-nokey.json" -w '%{http_code}' \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$admin_email\",\"password\":\"$admin_password\"}" \
  "http://localhost:$VERIFY_PORT/v1/admin/sessions")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/admin-nokey.json"; fail "a sign-in with no Idempotency-Key returned $status, want 400"; }
[[ "$(json "$WORKDIR/admin-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || { cat "$WORKDIR/admin-nokey.json"; fail "the refusal is not the middleware's"; }
ok "administrator sign-in is state-changing and is refused without an idempotency key"

# The buckets are shared with every section that runs after this one and with every concurrent
# worktree, so they are emptied rather than left to refill on a timer. Deliberately at the end and
# deliberately narrow: it removes this endpoint's keys and nothing else.
admin_clear_limits
[[ "$(admin_limit_keys | wc -l | tr -d ' ')" == "0" ]] \
  || fail "administrator sign-in buckets survived the clean-up"
ok "the administrator sign-in buckets are cleared, so a later section is not throttled by this one"

# ==========================================================================================
# SHIP-148 — granular permissions that default to the minimum.
#
# The Go tests hold the catalogue, the bundles and the default-deny property. **What only this can
# show is that the check is in front of the endpoint**, against the real binary, with a real session
# — a permission model whose middleware is never reached is a table nobody consults.
#
# It reuses SHIP-147's fixture hash and clears the same buckets, and it signs in twice, so it runs
# inside the same allowance the section above cleared at both ends.

ticket "SHIP-148  administrator permissions are granular and default to the minimum"

admin_clear_limits

owner_email="verify-admin-owner-$$@example.com"
owner_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$owner_email', 'Verify Owner Two', '$admin_fixture_hash', 'owner')
   returning id;")"
[[ -n "$owner_id" ]] || fail "the owner could not be created"

status="$(admin_signin "verify-adm-owner-in-$$" "$owner_email" "$admin_password" owner-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-owner-in.json"; fail "the owner could not sign in ($status)"; }
owner_token="$(json "$WORKDIR/admin-owner-in.json" '["token"]')"

# The permissions are published so a console can hide what it may not do, and they are the role's
# rather than a copy the client would have to derive.
owner_permissions="$(json "$WORKDIR/admin-owner-in.json" '["administrator"]["permissions"]')"
[[ "$owner_permissions" == *"admins.manage"* ]] \
  || { cat "$WORKDIR/admin-owner-in.json"; fail "the owner's permissions do not include admins.manage"; }
ok "an administrator is told what their role holds, expanded, so a console can hide what it may not do"

# --- creating an administrator, and the default that makes least privilege real -----------------

made_email="verify-admin-made-$$@example.com"
status="$(curl -s -X POST -o "$WORKDIR/admin-made.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $owner_token" -H "Idempotency-Key: verify-adm-made-$$" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$made_email\",\"name\":\"Verify Support\",\"password\":\"$admin_password\"}" \
  "http://localhost:$VERIFY_PORT/v1/admin/administrators")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/admin-made.json"; fail "creating an administrator returned $status, want 201"; }

made_role="$(json "$WORKDIR/admin-made.json" '["role"]')"
[[ "$made_role" == "support" ]] \
  || { cat "$WORKDIR/admin-made.json"; fail "an administrator created with no role stated got '$made_role', want the least-privileged role"; }
[[ "$(json "$WORKDIR/admin-made.json" '["permissions"]')" != *"admins.manage"* ]] \
  || { cat "$WORKDIR/admin-made.json"; fail "the created administrator inherited the creator's permission to manage administrators"; }
ok "an administrator created with no role stated gets the minimum, not the creator's — Docs/04 §9's least privilege"

# The same statement one level down: the column default is what covers a row that never came
# through the API at all, which is every migration and every support script.
# -qtAc, not -tAc: without -q psql appends the command tag, so this captures "support\nINSERT 0 1"
# and the comparison fails on a row that was written correctly. The same trap the SHIP-149 section
# at the top of this file records.
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into admin_users (id, email, name, password_hash)
   values (gen_random_uuid(), 'verify-admin-defaulted-$$@example.com', 'Defaulted', '$admin_fixture_hash')
   returning role;")" == "support" ]] \
  || fail "a row inserted with no role did not default to the least-privileged one"
ok "and so does a row inserted with no role at all, because the default is the column's as well as the code's"

# --- the granular refusal -----------------------------------------------------------------------

status="$(admin_signin "verify-adm-made-in-$$" "$made_email" "$admin_password" made-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-made-in.json"; fail "the created administrator could not sign in ($status)"; }
made_token="$(json "$WORKDIR/admin-made-in.json" '["token"]')"

# It reaches an endpoint that needs no special permission, so the refusal below is about the
# permission and not about the credential.
status="$(admin_get /v1/admin/me "$made_token" made-me)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-made-me.json"; fail "the support administrator cannot read its own session ($status)"; }

status="$(curl -s -X POST -o "$WORKDIR/admin-made-create.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $made_token" -H "Idempotency-Key: verify-adm-escalate-$$" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"verify-admin-escalated-$$@example.com\",\"name\":\"Escalated\",\"password\":\"$admin_password\",\"role\":\"owner\"}" \
  "http://localhost:$VERIFY_PORT/v1/admin/administrators")"
[[ "$status" == "403" ]] \
  || { cat "$WORKDIR/admin-made-create.json"; fail "a support administrator created an owner and got $status, want 403"; }
[[ "$(json "$WORKDIR/admin-made-create.json" '["error"]["code"]')" == "admin_permission_denied" ]] \
  || { cat "$WORKDIR/admin-made-create.json"; fail "the refusal is not a permission refusal"; }
[[ "$(cat "$WORKDIR/admin-made-create.json")" != *"admins.manage"* ]] \
  || { cat "$WORKDIR/admin-made-create.json"; fail "the refusal names the permission it wanted, so the console's surface is readable from the least-privileged account"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from admin_users where email = 'verify-admin-escalated-$$@example.com';")" == "0" ]] \
  || fail "the refused request created the account anyway"
ok "a granular permission refuses the one endpoint that could grant permissions, names nothing, and writes nothing"

# --- a role change takes effect on the next request, which is why the credential is a row --------

"$PSQL" "$DATABASE_URL" -q -c \
  "update admin_users set role = 'owner' where email = '$made_email';" >/dev/null \
  || fail "the role could not be changed"

status="$(admin_get /v1/admin/me "$made_token" made-promoted)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-made-promoted.json"; fail "the promoted administrator cannot read its own session ($status)"; }
[[ "$(json "$WORKDIR/admin-made-promoted.json" '["administrator"]["role"]')" == "owner" ]] \
  || { cat "$WORKDIR/admin-made-promoted.json"; fail "the same credential still reports the old role"; }
ok "a role change reaches the same unexpired credential on its very next request — a signed token would have carried the old one until it expired"

"$PSQL" "$DATABASE_URL" -q -c \
  "update admin_users set role = 'support' where email = '$made_email';" >/dev/null \
  || fail "the role could not be changed back"

status="$(curl -s -X POST -o "$WORKDIR/admin-demoted-create.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $made_token" -H "Idempotency-Key: verify-adm-demoted-$$" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"verify-admin-after-$$@example.com\",\"name\":\"After\",\"password\":\"$admin_password\"}" \
  "http://localhost:$VERIFY_PORT/v1/admin/administrators")"
[[ "$status" == "403" ]] \
  || { cat "$WORKDIR/admin-demoted-create.json"; fail "a demoted administrator kept the permission and got $status, want 403"; }
ok "and a demotion takes it away again on the next one, which is the direction that matters"

# --- the invariant SHIP-148 is most able to break -------------------------------------------------
#
# CLAUDE.md: audit entries are append-only and ordinary administrators cannot delete them. The Go
# half is that the catalogue has no such permission, so no endpoint can name one. **This is the half
# that holds for a connection that never went through the service at all**, which is what makes it a
# control rather than a convention — and it is the same trigger the SHIP-149 section above tests,
# re-asserted here because SHIP-148 is the ticket that would break it.

audit_entry="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into audit_log (id, actor_type, actor_id, action, target_type, target_id)
   values (gen_random_uuid(), 'admin', '$owner_id', 'administrator.created', 'admin_user', '$owner_id')
   returning id;")"
[[ -n "$audit_entry" ]] || fail "an audit entry could not be appended"

if "$PSQL" "$DATABASE_URL" -q -c \
  "delete from audit_log where id = '$audit_entry';" >/dev/null 2>&1; then
  fail "an audit entry naming an administrator was deleted"
fi
ok "no administrator can delete an audit entry — there is no permission for it and the trigger refuses it from any connection"

admin_clear_limits

# ==========================================================================================
# SHIP-117 — an exception-completed job enters the moderation queue.
#
# # Everything here is fenced by the proof id this section wrote
#
# The queue is a query over every exception in the database, and `make verify` runs against a
# database that is not reset between runs — so it contains SHIP-116's fixtures, SHIP-118's, every
# previous run's, and every other worktree's. A count would be a statement about the machine. The
# entry is found by its own id and the negative case is asserted on a photograph this section wrote,
# which is the same fencing rule the Kafka sections follow for the same reason.
#
# # The fixture is written with SQL because no endpoint reaches this state from here
#
# Recording an exception needs a driver token issued against an assignment (SHIP-107, SHIP-116), and
# 70-delivery.sh already demonstrates that path end to end. What this section needs is a *known* row
# to look for, so it writes one — in **one transaction**, because 000605's constraint trigger is
# deferred and a 'Delivered' milestone is checked for its evidence at COMMIT.

ticket "SHIP-117  an exception-completed job enters the moderation queue"

queue_job="$(dispute_delivered_job queue)"
queue_photo_job="$(dispute_delivered_job queuephoto)"

# queue_evidence <job> <reason-or-empty> — writes a milestone and its proof, answering with the
# proof id. An empty reason writes a photograph, which is the row that must NOT appear.
queue_evidence() {
  "$PSQL" "$DATABASE_URL" -qtA -v ON_ERROR_STOP=1 -v job="$1" -v reason="$2" <<'SQL'
BEGIN;
-- `recipient_name` and `delivery_note` are required on a 'Delivered' row from `000607` onwards
-- (SHIP-123), and refused on every other milestone. `Docs/01` §4.4 requires them of a delivered job
-- rather than of a delivery recorded through any particular route, so the constraint binds a fixture
-- as much as an endpoint. They are distinct from `reason`, which is the optional note above.
INSERT INTO milestones
    (id, job_id, milestone, actor_type, actor_id, reason, actor_recorded_at,
     recipient_name, delivery_note)
VALUES (gen_random_uuid(), :'job', 'Delivered', 'driver', gen_random_uuid(),
        'The recipient asked me not to photograph their door.', now(),
        'R. Chen', 'Left with reception, signed for');

INSERT INTO proofs (id, job_id, milestone_id, object_key, content_type, content_length, etag,
                    exception_reason)
SELECT gen_random_uuid(), :'job', m.id,
       CASE WHEN :'reason' = '' THEN 'proof/' || m.id || '.jpg' END,
       CASE WHEN :'reason' = '' THEN 'image/jpeg' END,
       CASE WHEN :'reason' = '' THEN 12345 END,
       CASE WHEN :'reason' = '' THEN 'etag-' || m.id END,
       CASE WHEN :'reason' = '' THEN NULL ELSE :'reason' END
  FROM milestones m
 WHERE m.job_id = :'job' AND m.milestone = 'Delivered'
 ORDER BY m.server_recorded_at DESC
 LIMIT 1;

SELECT p.id FROM proofs p WHERE p.job_id = :'job' ORDER BY p.created_at DESC LIMIT 1;
COMMIT;
SQL
}

queue_proof_id="$(queue_evidence "$queue_job" recipient_objected | tr -d '[:space:]')"
[[ -n "$queue_proof_id" ]] || fail "the exception fixture was not written"

queue_photo_id="$(queue_evidence "$queue_photo_job" "" | tr -d '[:space:]')"
[[ -n "$queue_photo_id" ]] || fail "the photograph fixture was not written"

# A support administrator, because reading a queue is what the least-privileged role exists to do.
queue_email="verify-admin-queue-$$@example.com"
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$queue_email', 'Verify Moderator', '$admin_fixture_hash', 'support');" >/dev/null \
  || fail "the queue reader could not be created"

status="$(admin_signin "verify-adm-queue-in-$$" "$queue_email" "$admin_password" queue-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-queue-in.json"; fail "the queue reader could not sign in ($status)"; }
queue_token="$(json "$WORKDIR/admin-queue-in.json" '["token"]')"

# Paged through until the fixture is found or the pages run out, because the queue holds every
# exception this database has ever seen and the entry is fenced by id rather than by position.
queue_found=""
queue_photo_seen=""
queue_cursor=""
for _ in $(seq 1 40); do
  path="/v1/admin/moderation/exceptions?limit=50"
  [[ -n "$queue_cursor" ]] && path="$path&cursor=$queue_cursor"

  status="$(admin_get "$path" "$queue_token" queue-page)"
  [[ "$status" == "200" ]] || { cat "$WORKDIR/admin-queue-page.json"; fail "reading the exception queue returned $status"; }

  python3 - "$WORKDIR/admin-queue-page.json" "$queue_proof_id" "$queue_photo_id" >"$WORKDIR/queue-scan.txt" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
items = page.get("data") or []
found = next((i for i in items if i["entry_id"] == sys.argv[2]), None)
photo = any(i["entry_id"] == sys.argv[3] for i in items)
print(json.dumps(found) if found else "")
print("photo" if photo else "")
print(page.get("next_cursor") or "")
PY
  queue_entry="$(sed -n '1p' "$WORKDIR/queue-scan.txt")"
  [[ -n "$(sed -n '2p' "$WORKDIR/queue-scan.txt")" ]] && queue_photo_seen="yes"
  queue_cursor="$(sed -n '3p' "$WORKDIR/queue-scan.txt")"

  [[ -n "$queue_entry" ]] && { queue_found="$queue_entry"; break; }
  [[ -n "$queue_cursor" ]] || break
done

[[ -n "$queue_found" ]] || fail "the exception-completed job never appeared in the moderation queue"
printf '%s' "$queue_found" >"$WORKDIR/queue-entry.json"

[[ "$(json "$WORKDIR/queue-entry.json" '["job_id"]')" == "$queue_job" ]] \
  || { cat "$WORKDIR/queue-entry.json"; fail "the entry names the wrong job"; }
[[ "$(json "$WORKDIR/queue-entry.json" '["reason"]')" == "recipient_objected" ]] \
  || { cat "$WORKDIR/queue-entry.json"; fail "the entry does not carry the reason that was recorded"; }
[[ "$(json "$WORKDIR/queue-entry.json" '["milestone"]')" == "Delivered" ]] \
  || { cat "$WORKDIR/queue-entry.json"; fail "the entry does not say which claim the exception stands behind"; }
[[ -n "$(json "$WORKDIR/queue-entry.json" '["note"]')" ]] \
  || { cat "$WORKDIR/queue-entry.json"; fail "the driver's own words are not carried"; }
[[ "$(json "$WORKDIR/queue-entry.json" '["ground"]')" == "failed_proof" ]] \
  || { cat "$WORKDIR/queue-entry.json"; fail "the entry does not say which ground it is here on"; }
ok "a delivery evidenced by a reason rather than a photograph is in the moderation queue — SHIP-117's Done when, on the wire"

[[ -z "$queue_photo_seen" ]] \
  || fail "a delivery evidenced by a photograph is in the exception queue, so the queue is every delivery"
ok "and one evidenced by a photograph is not — Docs/04 §5's fourth queue is failed proof of delivery, not every delivery"

# Nothing about money, and nothing about the photograph that does not exist. A queue entry is what
# somebody triaging needs, and a customer's budget is never exposed to anybody through any endpoint.
for forbidden in budget amount price object_key url; do
  [[ "$(cat "$WORKDIR/queue-entry.json")" != *"\"$forbidden\""* ]] \
    || { cat "$WORKDIR/queue-entry.json"; fail "the queue entry carries a $forbidden field"; }
done
ok "the entry carries no budget and no object key — there is no photograph to point at, which is why the row exists"

# The permission, on the wire. Every role holds moderation.read, so what is being shown here is that
# the check is in front of the endpoint at all — the demotion case above is where a refusal is shown.
status="$(admin_get /v1/admin/moderation/exceptions "" queue-nocred)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/admin-queue-nocred.json"; fail "the queue is readable without a credential ($status)"; }
ok "and the queue is behind the administrator credential like every other administrative route"

admin_clear_limits

# ==========================================================================================
# SHIP-157 — the other three grounds, and the queue as one screen.
#
# # What only this section can show
#
# The queue is a `UNION ALL` of four selects that live in **cmd/api**, because they span `jobs`,
# `proofs` and `milestones` — tables belonging to two domains `internal/admin` may not import. The Go
# suite in that package drives the handler against a copy of the statement, and a text guard holds
# the copy to the original; that establishes that the two agree and nothing about the one the binary
# runs. **The served statement is demonstrated here or nowhere**, exactly as SHIP-117's third of it
# was.
#
# It also shows the one thing no unit test can: that the threshold the running process passes is
# `delivery.UnsyncedAlertThreshold` rather than a number `internal/admin` invented. The domain's
# tests pass whatever threshold they like — that is what makes them a test of the predicate — and
# only the wired binary reads the real constant.
#
# # Every assertion is fenced on this run's own rows
#
# `jobs`, `proofs` and `milestones` are shared with every other section, every previous run and every
# other worktree, so a count would be a statement about the machine. Each fixture is found by the id
# this section created, and each negative is asserted on a row it also created.

ticket "SHIP-157  overdue pickup, delayed delivery, failed proof and unsynced milestones are one queue"

admin_clear_limits

# queue_find <ground-filter> <entry-id> — the ground the entry is on, or "" when it is not there.
#
# Pages to the end rather than reading the first page: the queue holds every exception this database
# has ever seen, and a fixture written now sorts by its own recorded_at rather than to the front.
queue_find() {
  local filter="$1" wanted="$2" cursor="" path found=""

  for _ in $(seq 1 60); do
    path="/v1/admin/moderation/exceptions?limit=100"
    [[ -n "$filter" ]] && path="$path&ground=$filter"
    [[ -n "$cursor" ]] && path="$path&cursor=$cursor"

    status="$(admin_get "$path" "$queue_token" queue-find)"
    [[ "$status" == "200" ]] || { cat "$WORKDIR/admin-queue-find.json"; fail "reading the queue returned $status"; }

    python3 - "$WORKDIR/admin-queue-find.json" "$wanted" "$filter" >"$WORKDIR/queue-find.txt" <<'SCAN'
import json, sys
page = json.load(open(sys.argv[1]))
items = page.get("data") or []
match = next((i for i in items if i["entry_id"] == sys.argv[2]), None)
# A filtered page carrying an entry on another ground is a filter that is not filtering.
stray = [i["ground"] for i in items if sys.argv[3] and i["ground"] != sys.argv[3]]
print(match["ground"] if match else "")
print(",".join(sorted(set(stray))))
print(page.get("next_cursor") or "")
SCAN

    [[ -z "$(sed -n '2p' "$WORKDIR/queue-find.txt")" ]] \
      || fail "a page filtered to '$filter' carries entries on $(sed -n '2p' "$WORKDIR/queue-find.txt")"

    found="$(sed -n '1p' "$WORKDIR/queue-find.txt")"
    [[ -n "$found" ]] && { printf '%s' "$found"; return; }

    cursor="$(sed -n '3p' "$WORKDIR/queue-find.txt")"
    [[ -n "$cursor" ]] || break
  done
  printf ''
}

# --- grounds one and two: the window grounds ----------------------------------------------------
#
# The windows are written with SQL because no endpoint sets one after award, and each job is left at
# the status its ground needs — which is what tells the two apart rather than firing both on one job.

overdue_job="$(dispute_awarded_job overdue)"
"$PSQL" "$DATABASE_URL" -q -c \
  "update jobs set pickup_window_end = now() - interval '2 days',
                   dropoff_window_end = now() + interval '2 days'
     where id = '$overdue_job';" >/dev/null \
  || fail "the overdue job's windows could not be set"

ontime_job="$(dispute_awarded_job ontime)"
"$PSQL" "$DATABASE_URL" -q -c \
  "update jobs set pickup_window_end = now() + interval '2 days',
                   dropoff_window_end = now() + interval '3 days'
     where id = '$ontime_job';" >/dev/null \
  || fail "the on-time job's windows could not be set"

[[ "$(queue_find "" "$overdue_job")" == "overdue_pickup" ]] \
  || fail "a job past its pickup window with no collection is not in the queue on overdue_pickup"
ok "a job past its pickup window that has not been collected is in the queue — Docs/04 §5's first ground"

[[ -z "$(queue_find "" "$ontime_job")" ]] \
  || fail "a job whose windows have not closed is in the queue, so the predicate is missing"
ok "and one whose windows have not closed is not — every predicate here passes trivially when it is absent"

# Picked up and still moving: it cannot be overdue for its pickup and it can be late for its
# delivery, which is the pair of facts that separates the two window grounds.
transit_job="$(dispute_transit_job delayed)"
"$PSQL" "$DATABASE_URL" -q -c \
  "update jobs set pickup_window_end = now() - interval '3 days',
                   dropoff_window_end = now() - interval '2 days'
     where id = '$transit_job';" >/dev/null \
  || fail "the in-transit job's windows could not be set"

[[ "$(queue_find "" "$transit_job")" == "delayed_delivery" ]] \
  || fail "a job past its drop-off window still in transit is not in the queue on delayed_delivery"
ok "a job past its drop-off window that has not been delivered is in the queue — Docs/04 §5's second ground"

# A delivered job past its drop-off window arrived. It is late history rather than a delivery going
# wrong, and a queue that carried it would fill with every completed job the platform has ever run.
arrived_job="$(dispute_delivered_job arrived)"
"$PSQL" "$DATABASE_URL" -q -c \
  "update jobs set pickup_window_end = now() - interval '3 days',
                   dropoff_window_end = now() - interval '2 days'
     where id = '$arrived_job';" >/dev/null \
  || fail "the arrived job's windows could not be set"

[[ -z "$(queue_find delayed_delivery "$arrived_job")" ]] \
  || fail "a delivered job past its drop-off window is on the delayed_delivery ground; it arrived"
ok "and a job that was delivered is not, whatever its window says — the queue is deliveries going wrong, not late history"

# --- ground four: the unsynced milestone, and the threshold the process actually holds -----------
#
# server_recorded_at cannot be supplied — 000601's trigger refuses an INSERT that names it — so the
# gap is made by backdating actor_recorded_at, which is how a real one arises: the handset recorded
# it then and the platform saw it now.

unsynced_job="$(dispute_transit_job unsynced)"

unsynced_milestone() {
  "$PSQL" "$DATABASE_URL" -qtAc \
    "insert into milestones (id, job_id, milestone, actor_type, actor_id, actor_recorded_at)
     values (gen_random_uuid(), '$1', 'In transit', 'driver', gen_random_uuid(),
             now() - interval '$2')
     returning id;"
}

late_milestone="$(unsynced_milestone "$unsynced_job" '25 hours' | tr -d '[:space:]')"
[[ -n "$late_milestone" ]] || fail "the late milestone fixture was not written"

prompt_milestone="$(unsynced_milestone "$unsynced_job" '1 minute' | tr -d '[:space:]')"
[[ -n "$prompt_milestone" ]] || fail "the prompt milestone fixture was not written"

# 23 hours is the case that pins the threshold to twenty-four rather than to "some hours". A process
# wired with an hour, or with a number internal/admin invented, puts this on the queue.
near_milestone="$(unsynced_milestone "$unsynced_job" '23 hours' | tr -d '[:space:]')"
[[ -n "$near_milestone" ]] || fail "the near-threshold milestone fixture was not written"

[[ "$(queue_find "" "$late_milestone")" == "unsynced_milestone" ]] \
  || fail "an update that reached the platform 25 hours after it was recorded is not in the queue"
ok "an update that reached the platform more than a day after it was recorded is in the queue — SHIP-128's fact, surfaced"

[[ -z "$(queue_find "" "$prompt_milestone")" ]] \
  || fail "an update that arrived a minute after it was recorded is in the unsynced queue"
[[ -z "$(queue_find "" "$near_milestone")" ]] \
  || fail "an update 23 hours behind is in the queue, so the threshold the process holds is not Docs/02 §3.1's 24 hours"
ok "and one 23 hours behind is not — the running process holds delivery.UnsyncedAlertThreshold rather than a number admin invented"

# --- the filter, in both directions -------------------------------------------------------------

[[ "$(queue_find overdue_pickup "$overdue_job")" == "overdue_pickup" ]] \
  || fail "filtering to overdue_pickup excluded the overdue job"
[[ -z "$(queue_find overdue_pickup "$late_milestone")" ]] \
  || fail "filtering to overdue_pickup returned an unsynced milestone"
ok "the ground filter narrows the one queue and returns nothing from the other three"

status="$(admin_get "/v1/admin/moderation/exceptions?ground=overdue_pickups" "$queue_token" queue-bad-ground)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/admin-queue-bad-ground.json"; fail "a mistyped ground returned $status, want 422"; }
[[ "$(cat "$WORKDIR/admin-queue-bad-ground.json")" == *'"ground"'* ]] \
  || { cat "$WORKDIR/admin-queue-bad-ground.json"; fail "the refusal does not name the field"; }
ok "a ground the platform does not have is refused and names the field, rather than answering with every entry"

# A cursor issued before the widening is two fields where three are needed. It is refused rather
# than paged wrongly — a client holding one has a bug, and answering it would page for ever through
# a queue it never saw.
old_cursor="$(printf '%s|%s' '2026-08-14T02:15:30.000Z' "$queue_proof_id" | openssl base64 -A | tr '+/' '-_' | tr -d '=')"
status="$(admin_get "/v1/admin/moderation/exceptions?cursor=$old_cursor" "$queue_token" queue-old-cursor)"
[[ "$status" == "400" ]] \
  || { cat "$WORKDIR/admin-queue-old-cursor.json"; fail "a two-field cursor returned $status, want 400"; }
ok "and a cursor from before the queue was widened is refused rather than paging wrongly"

admin_clear_limits

# ==========================================================================================
# SHIP-158 — the post-award cancellation queue.
#
# # What only this section can show
#
# The statement is a four-CTE read living in **cmd/api**, over `job_status_history`, `bids` and
# `jobs` — three tables belonging to two domains `internal/admin` may not import. The Go suite in
# that package drives the handler against a copy held to the original by a text guard, which
# establishes that the two agree and nothing about the one the binary runs.
#
# # The finding this ticket turned on, demonstrated rather than asserted
#
# **Docs/02 §2 has no `Awarded → Cancelled` transition.** So a queue keyed on a job reaching
# `Cancelled` would list the pre-award cancellations and none of the post-award ones — the exact
# inverse of the ticket. The negatives below are what say this queue is not that one: a job cancelled
# from `Open` and a draft the customer abandoned are both refused, and both are what the wrong
# implementation returns.
#
# # Every assertion is fenced on this run's own rows
#
# `job_status_history` is shared with every other section, every previous run and every other
# worktree, so a count would be a statement about the machine. Each job is found by the id this
# section created.

ticket "SHIP-158  cancellations after award are listed with the provider's history"

admin_clear_limits

# cancel_find <job-id> — the outcome the job is on the queue with, or "" when it is not there.
cancel_find() {
  local wanted="$1" cursor="" path found=""

  for _ in $(seq 1 60); do
    path="/v1/admin/moderation/cancellations?limit=100"
    [[ -n "$cursor" ]] && path="$path&cursor=$cursor"

    status="$(admin_get "$path" "$queue_token" cancel-find)"
    [[ "$status" == "200" ]] || { cat "$WORKDIR/admin-cancel-find.json"; fail "reading the cancellation queue returned $status"; }

    python3 - "$WORKDIR/admin-cancel-find.json" "$wanted" >"$WORKDIR/cancel-find.txt" <<'SCAN'
import json, sys
page = json.load(open(sys.argv[1]))
items = page.get("data") or []
match = next((i for i in items if i["job_id"] == sys.argv[2]), None)
print(match["outcome"] if match else "")
print(json.dumps(match) if match else "")
print(page.get("next_cursor") or "")
SCAN

    found="$(sed -n '1p' "$WORKDIR/cancel-find.txt")"
    if [[ -n "$found" ]]; then
      sed -n '2p' "$WORKDIR/cancel-find.txt" >"$WORKDIR/cancel-entry.json"
      printf '%s' "$found"
      return
    fi

    cursor="$(sed -n '3p' "$WORKDIR/cancel-find.txt")"
    [[ -n "$cursor" ]] || break
  done
  printf ''
}

# --- the two shapes Docs/02 permits -------------------------------------------------------------
#
# Both are driven through dispute_move, which is the guarded transition function — 000402 refuses a
# status written any other way, so a fixture that wrote one would be testing a queue over rows the
# platform cannot produce. Neither shape has an endpoint yet: provider cancellation has no ticket and
# dispute resolution is SHIP-164, which is exactly why the queue is a query over rows the guard
# already permits rather than a table something has to remember to write to.

walked_job="$(dispute_awarded_job walked)"
dispute_move_because "$walked_job" Awarded Open provider \
  'The provider could not source a vehicle with a tailgate lifter.'

ended_job="$(dispute_awarded_job ended)"
dispute_move "$ended_job" Awarded Disputed
dispute_move_because "$ended_job" Disputed Cancelled admin \
  'Resolved as a failed delivery; the goods never left the depot.'

[[ "$(cancel_find "$walked_job")" == "returned_to_market" ]] \
  || fail "a provider cancellation after award is not in the queue on returned_to_market"
[[ "$(json "$WORKDIR/cancel-entry.json" '["from_status"]')" == "Awarded" ]] \
  || { cat "$WORKDIR/cancel-entry.json"; fail "the entry does not say which status the job left"; }
[[ "$(json "$WORKDIR/cancel-entry.json" '["provider_id"]')" == "$dispute_provider_id" ]] \
  || { cat "$WORKDIR/cancel-entry.json"; fail "the entry names the wrong provider"; }
[[ -n "$(json "$WORKDIR/cancel-entry.json" '["reason"]')" ]] \
  || { cat "$WORKDIR/cancel-entry.json"; fail "the recorded reason is not carried"; }
ok "a provider cancellation after award is in the queue — Docs/02 §6.2's signal, which cannot be reconstructed later"

[[ "$(cancel_find "$ended_job")" == "ended" ]] \
  || fail "a job cancelled through a dispute after award is not in the queue on ended"
[[ "$(json "$WORKDIR/cancel-entry.json" '["from_status"]')" == "Disputed" ]] \
  || { cat "$WORKDIR/cancel-entry.json"; fail "the ended entry does not report the status it left"; }
ok "and so is one ended through a dispute — Docs/02 §2's only route to Cancelled after award"

# --- the negatives, which are what the wrong implementation returns ------------------------------

open_cancelled="$(dispute_draft opencancel)"
dispute_move "$open_cancelled" Draft Open
dispute_move "$open_cancelled" Open Cancelled

abandoned_draft="$(dispute_draft abandoned)"
dispute_move "$abandoned_draft" Draft Cancelled

[[ -z "$(cancel_find "$open_cancelled")" ]] \
  || fail "a job cancelled from Open is in the post-award queue, so it is a list of every cancelled job"
[[ -z "$(cancel_find "$abandoned_draft")" ]] \
  || fail "an abandoned draft is in the post-award queue"
ok "and a job cancelled from Open and an abandoned draft are not — there is no Awarded to Cancelled transition, so those are what a naive query returns"

# --- the provider's history, which is the clause a queue of job ids would not meet ----------------
#
# Two cancellations against one completion for this run's provider. The figures are read off the
# entry rather than counted here: what is being shown is that the endpoint computes them, and a
# count taken from the database would be this section agreeing with itself.

completed_job="$(dispute_delivered_job completedone)"
dispute_move "$completed_job" Delivered Completed

cancel_find "$walked_job" >/dev/null
cancellations="$(json "$WORKDIR/cancel-entry.json" '["provider_cancellations"]')"
completions="$(json "$WORKDIR/cancel-entry.json" '["provider_completions"]')"

[[ "$cancellations" -ge 2 ]] \
  || { cat "$WORKDIR/cancel-entry.json"; fail "provider_cancellations is $cancellations, and this run made two"; }
[[ "$completions" -ge 1 ]] \
  || { cat "$WORKDIR/cancel-entry.json"; fail "provider_completions is $completions, and this run completed one"; }
ok "the entry carries the provider's history — the count and its denominator, because three cancellations against four hundred deliveries is not three against five"

status="$(admin_get /v1/admin/moderation/cancellations "" cancel-nocred)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/admin-cancel-nocred.json"; fail "the cancellation queue is readable without a credential ($status)"; }
ok "and the queue is behind the administrator credential like every other administrative route"

# Nothing commercial. A queue entry says who walked away and how often; a price is the job console's
# disclosure decision and a customer's budget is nobody's.
cancel_find "$walked_job" >/dev/null
for forbidden in budget amount price bid_amount; do
  [[ "$(cat "$WORKDIR/cancel-entry.json")" != *"\"$forbidden\""* ]] \
    || { cat "$WORKDIR/cancel-entry.json"; fail "the cancellation entry carries a $forbidden field"; }
done
ok "the entry carries no budget and no bid amount — a shape that never carried one cannot leak one"

admin_clear_limits

# ==========================================================================================
# SHIP-166 — two-person review for permanent suspension.
#
# # What only this section can show
#
# The Go suite drives both endpoints against a real database and is the stronger of the two: it
# asserts the account is untouched by a request, that the requester's own approval is refused, and
# that the review stays pending afterwards. **What it cannot show is that the routes are wired at
# all** — a handler built and never registered compiles, passes every test in `internal/admin`, and
# serves a 404 to the console. It also cannot show that `POST /v1/admin/users/{id}/standing` refuses
# `suspended` on the built binary, which is the half a console meets first.
#
# # Every assertion is fenced on this run's own rows
#
# `suspension_reviews` is shared with every previous run and every other worktree. Each review is
# found by the id this section created.

ticket "SHIP-166  a permanent suspension requires a second administrator's approval"

admin_clear_limits

# Two moderators, because one cannot do this — which is the ticket. Both hold users.restrict; the
# control is that there are two people, not that either holds something the other does not.
requester_email="verify-susp-a-$$@example.com"
approver_email="verify-susp-b-$$@example.com"
for email in "$requester_email" "$approver_email"; do
  "$PSQL" "$DATABASE_URL" -q -c \
    "insert into admin_users (id, email, name, password_hash, role)
     values (gen_random_uuid(), '$email', 'Verify Reviewer', '$admin_fixture_hash', 'moderator');" >/dev/null \
    || fail "the reviewing administrator $email could not be created"
done

status="$(admin_signin "verify-susp-a-in-$$" "$requester_email" "$admin_password" susp-a-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-susp-a-in.json"; fail "the requester could not sign in ($status)"; }
requester_token="$(json "$WORKDIR/admin-susp-a-in.json" '["token"]')"
requester_id="$(json "$WORKDIR/admin-susp-a-in.json" '["administrator"]["id"]')"

status="$(admin_signin "verify-susp-b-in-$$" "$approver_email" "$admin_password" susp-b-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-susp-b-in.json"; fail "the approver could not sign in ($status)"; }
approver_token="$(json "$WORKDIR/admin-susp-b-in.json" '["token"]')"
approver_id="$(json "$WORKDIR/admin-susp-b-in.json" '["administrator"]["id"]')"

[[ "$requester_id" != "$approver_id" ]] \
  || fail "the fixture signed in one administrator twice, so this section would prove nothing"

# The account under review, registered through the endpoint so that it is an ordinary account rather
# than a row this section wrote.
susp_email="verify-susp-subject-$$@example.com"
status="$(post_json "verify-susp-reg-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$susp_email\",\"phone\":\"04196$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/susp-subject.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/susp-subject.json"; fail "the account under review could not be registered: $status"; }
susp_user_id="$(json "$WORKDIR/susp-subject.json" '["id"]')"

# standing_of <user-id> — the column identity reads at sign-in and at refresh.
standing_of() {
  "$PSQL" "$DATABASE_URL" -qtAc "select status from users where id = '$1';" | tr -d '[:space:]'
}

# --- the old route no longer suspends ------------------------------------------------------------

status="$(admin_post "/v1/admin/users/$susp_user_id/standing" "$requester_token" \
  "verify-susp-old-$$" '{"standing":"suspended","reason":"Three unresolved safety reports in a fortnight."}' \
  susp-old)"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/admin-susp-old.json"; fail "the standing endpoint suspended on one administrator's say-so ($status)"; }
[[ "$(cat "$WORKDIR/admin-susp-old.json")" == *'admin_suspension_needs_review'* ]] \
  || { cat "$WORKDIR/admin-susp-old.json"; fail "the refusal does not tell the console where to go"; }
[[ "$(standing_of "$susp_user_id")" == "active" ]] \
  || fail "the account was suspended by the endpoint that refused to suspend it"
ok "one administrator can no longer suspend through the standing endpoint, and is told where to go instead"

# --- the request changes nothing -----------------------------------------------------------------

status="$(admin_post "/v1/admin/users/$susp_user_id/suspension" "$requester_token" \
  "verify-susp-req-$$" '{"reason":"Three unresolved safety reports in a fortnight."}' susp-req)"
[[ "$status" == "202" ]] \
  || { cat "$WORKDIR/admin-susp-req.json"; fail "requesting a suspension returned $status, want 202"; }
review_id="$(json "$WORKDIR/admin-susp-req.json" '["id"]')"
[[ -n "$review_id" ]] || fail "the request returned no review"
[[ "$(json "$WORKDIR/admin-susp-req.json" '["status"]')" == "pending" ]] \
  || fail "a new review is not pending"

[[ "$(standing_of "$susp_user_id")" == "active" ]] \
  || fail "the account was suspended by the *request*, so the second signature is paperwork"
ok "a request records the case and leaves the account alone — the control is not a suspension with a signature collected afterwards"

# --- the queue, without which nobody can find the request ----------------------------------------

status="$(admin_get /v1/admin/suspensions "$approver_token" susp-queue)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-susp-queue.json"; fail "reading the queue returned $status"; }
python3 - "$WORKDIR/admin-susp-queue.json" "$review_id" >"$WORKDIR/susp-in-queue.txt" <<'SCAN'
import json, sys
page = json.load(open(sys.argv[1]))
match = next((r for r in (page.get("data") or []) if r["id"] == sys.argv[2]), None)
print(match["status"] if match else "")
SCAN
[[ "$(cat "$WORKDIR/susp-in-queue.txt")" == "pending" ]] \
  || fail "the pending review is not in the queue — a second administrator cannot approve what they cannot find"
ok "and the second administrator can find it in the queue, which is what makes the control exercisable"

# --- the requester cannot approve their own request ----------------------------------------------
#
# The one check this whole ticket exists for. It is refused three deep — in the service, in the
# UPDATE's own predicate, and by ck_suspension_reviews_two_people — and only the built binary shows
# that the refusal survives the wiring.

status="$(admin_post "/v1/admin/suspensions/$review_id/approval" "$requester_token" \
  "verify-susp-self-$$" '{}' susp-self)"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/admin-susp-self.json"; fail "one administrator completed a two-person review alone ($status)"; }
[[ "$(cat "$WORKDIR/admin-susp-self.json")" == *'admin_same_administrator'* ]] \
  || { cat "$WORKDIR/admin-susp-self.json"; fail "the refusal does not say a second administrator is needed"; }
[[ "$(standing_of "$susp_user_id")" == "active" ]] \
  || fail "the account was suspended by a refused approval, which is the partial write the transaction prevents"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc "select status from suspension_reviews where id = '$review_id';" | tr -d '[:space:]')" == "pending" ]] \
  || fail "the review is no longer pending after a refused approval, so nobody else can act on it"
ok "the administrator who asked cannot be the one who agrees — Docs/04 §9, refused on the wire with the account untouched"

# --- a second administrator can, and the suspension takes effect ----------------------------------

status="$(admin_post "/v1/admin/suspensions/$review_id/approval" "$approver_token" \
  "verify-susp-ok-$$" '{}' susp-ok)"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/admin-susp-ok.json"; fail "the second administrator could not approve the review ($status)"; }
[[ "$(json "$WORKDIR/admin-susp-ok.json" '["approved_by"]')" == "$approver_id" ]] \
  || { cat "$WORKDIR/admin-susp-ok.json"; fail "the review names the wrong approver"; }
[[ "$(json "$WORKDIR/admin-susp-ok.json" '["requested_by"]')" == "$requester_id" ]] \
  || { cat "$WORKDIR/admin-susp-ok.json"; fail "the review lost the administrator who asked"; }
[[ "$(standing_of "$susp_user_id")" == "suspended" ]] \
  || fail "the account is not suspended after a valid approval"
ok "a second administrator's approval applies the suspension, and the review names both people"

# The suspension is real where it matters: identity refuses the account at sign-in. SHIP-161
# established that path and this is what says a two-person review reaches it.
status="$(post_json "verify-susp-signin-$$" /v1/auth/login \
  "{\"email\":\"$susp_email\",\"password\":\"correct-horse-battery-staple\",\"device_label\":\"iPhone\"}" \
  "$WORKDIR/susp-signin.json")"
[[ "$status" != "200" ]] \
  || { cat "$WORKDIR/susp-signin.json"; fail "a suspended account signed in, so the review changed a column nothing reads"; }
ok "and the account cannot sign in — the review reaches the enforcement identity already had"

# Refresh too, which is where a suspension actually takes effect: an access token lives fifteen
# minutes and carries no standing, so an account refused only at sign-in keeps working until the
# token it already holds expires. This half moved here from the SHIP-161 section, because the
# two-person route is now the only way to reach a suspension at all.
status="$(post_json "verify-susp-login-$$" /v1/auth/login \
  "{\"email\":\"$susp_email\",\"password\":\"correct-horse-battery-staple\",\"device_label\":\"Verify Suspension\"}" \
  "$WORKDIR/susp-login-refused.json")"
[[ "$status" == "403" ]] \
  || { cat "$WORKDIR/susp-login-refused.json"; fail "a suspended account signed in and got $status, want 403"; }
ok "and the refusal is a 403 rather than a bad-credentials answer — the account exists and may not be used"

# --- the audit trail names both administrators ----------------------------------------------------

approved_entries="$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log
    where action = 'user.suspension_approved'
      and target_id = '$susp_user_id'
      and actor_id = '$approver_id'
      and metadata ->> 'requested_by' = '$requester_id';")"
[[ "$approved_entries" == "1" ]] \
  || fail "the approval entry does not name both administrators (found $approved_entries)"

requested_entries="$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log
    where action = 'user.suspension_requested'
      and target_id = '$susp_user_id'
      and actor_id = '$requester_id';")"
[[ "$requested_entries" == "1" ]] \
  || fail "the request wrote no audit entry (found $requested_entries)"
ok "both halves are in the trail — a two-person control that recorded one name would not have recorded what happened"

# --- a settled review cannot be approved again ----------------------------------------------------

status="$(admin_post "/v1/admin/suspensions/$review_id/approval" "$requester_token" \
  "verify-susp-again-$$" '{}' susp-again)"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/admin-susp-again.json"; fail "a settled review was approved again ($status)"; }
ok "and a settled review cannot be approved twice — the record of who agreed cannot be rewritten"

admin_clear_limits

# ==========================================================================================
# SHIP-150 — every admin mutation writes an audit entry.
#
# # What only this section can show
#
# The Go suite in internal/admin drives each mutation through the handler and reads the row back,
# and it is the stronger of the two: it asserts the entry names the acting administrator, the
# target, and the injected clock's instant. **What it cannot show is that the wiring in cmd/api
# hands the domain a writer at all.** `adminHandler` builds the auditor from `d.Clock` and passes it
# to `NewCredentials`; a change that passed a fresh `clock.System{}`, or that constructed a
# credentials service somewhere else, compiles and passes every Go test in the domain. Against the
# built binary it does not.
#
# # Everything is fenced on this run's own administrator
#
# `make verify` runs against a database that is not reset between runs, so `audit_log` holds every
# previous run's entries and every other worktree's. A count over the table would be a statement
# about the machine. Each check below counts entries **for one actor created by this run**, which is
# the same fencing rule the Kafka and moderation-queue sections follow.

ticket "SHIP-150  every privileged administrative action writes an audit entry"

admin_clear_limits

# audit_count <action> <actor-id> — how many entries this actor has for this action.
#
# -qtAc, not -tAc. Without -q psql appends the command tag and this captures "1\nSELECT 1", which
# the comparisons below fail on for a row that was written perfectly. The same trap the SHIP-149
# section at the top of this file records, and it has now cost two sections.
audit_count() {
  "$PSQL" "$DATABASE_URL" -qtAc \
    "select count(*) from audit_log where action = '$1' and actor_type = 'admin' and actor_id = '$2';"
}

audit_email="verify-admin-audit-$$@example.com"
audit_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$audit_email', 'Verify Auditor', '$admin_fixture_hash', 'owner')
   returning id;")"
[[ -n "$audit_id" ]] || fail "the audit fixture administrator could not be created"

# Created by an INSERT rather than through the endpoint, which is `000801`'s bootstrap path — so
# there is no entry for it yet, and the count below starts at a known zero.
[[ "$(audit_count administrator.signed_in "$audit_id")" == "0" ]] \
  || fail "the audit fixture administrator already has sign-in entries, so nothing below is fenced"

# --- sign-in ------------------------------------------------------------------------------------

status="$(admin_signin "verify-adm-audit-in-$$" "$audit_email" "$admin_password" audit-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-audit-in.json"; fail "the audit fixture could not sign in ($status)"; }
audit_token="$(json "$WORKDIR/admin-audit-in.json" '["token"]')"

[[ "$(audit_count administrator.signed_in "$audit_id")" == "1" ]] \
  || fail "signing in wrote no audit entry, so nothing records who was in the console"
ok "an administrator signing in is recorded against their own account"

# The entry carries the clock the rest of the row does. `created_at` is DEFAULT now() in the schema
# and is supplied by the domain from the injected clock (Docs/11 §9 — one row, one clock), so in a
# live service it is within seconds of the session it describes. A minute is generous and would
# still catch a column left to a default on a host whose clock had drifted.
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log a
     join admin_sessions s on s.admin_user_id = a.actor_id
    where a.actor_id = '$audit_id'
      and a.action = 'administrator.signed_in'
      and abs(extract(epoch from (a.created_at - s.created_at))) < 60;")" != "0" ]] \
  || fail "the audit entry's instant does not agree with the session it describes"
ok "and its instant agrees with the session row, because both come from one clock"

# --- creating an administrator, which is the action that grants every other permission -----------

audit_made_email="verify-admin-audited-made-$$@example.com"
status="$(curl -s -X POST -o "$WORKDIR/admin-audit-made.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $audit_token" -H "Idempotency-Key: verify-adm-audit-made-$$" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$audit_made_email\",\"name\":\"Audited\",\"password\":\"$admin_password\",\"role\":\"moderator\"}" \
  "http://localhost:$VERIFY_PORT/v1/admin/administrators")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/admin-audit-made.json"; fail "creating an administrator returned $status, want 201"; }
audit_made_id="$(json "$WORKDIR/admin-audit-made.json" '["id"]')"

[[ "$(audit_count administrator.created "$audit_id")" == "1" ]] \
  || fail "creating an administrator wrote no audit entry"
ok "creating an administrator is recorded against the administrator who created it"

# The target and the metadata, which are what make the entry worth reading. "An account was
# created" is half a fact; "an account was created that can restrict users" is the other half, and
# Docs/04 §9's least-privilege control is unauditable without it.
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log
    where actor_id = '$audit_id'
      and action = 'administrator.created'
      and target_type = 'administrator'
      and target_id = '$audit_made_id'
      and metadata->>'role' = 'moderator';")" == "1" ]] \
  || fail "the entry does not name the account created or the role it was given"
ok "and it names the account created and the role it was granted"

# --- a refused action writes nothing --------------------------------------------------------------
#
# The direction that matters. An entry written before the work, or outside the transaction, would
# record an action that did not happen — and an append-only table has no way to take it back.

status="$(admin_signin "verify-adm-audit-made-in-$$" "$audit_made_email" "$admin_password" audit-made-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-audit-made-in.json"; fail "the created administrator could not sign in ($status)"; }
audit_made_token="$(json "$WORKDIR/admin-audit-made-in.json" '["token"]')"

status="$(curl -s -X POST -o "$WORKDIR/admin-audit-refused.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $audit_made_token" -H "Idempotency-Key: verify-adm-audit-refused-$$" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"verify-admin-never-$$@example.com\",\"name\":\"Never\",\"password\":\"$admin_password\"}" \
  "http://localhost:$VERIFY_PORT/v1/admin/administrators")"
[[ "$status" == "403" ]] \
  || { cat "$WORKDIR/admin-audit-refused.json"; fail "a moderator created an administrator and got $status, want 403"; }
[[ "$(audit_count administrator.created "$audit_made_id")" == "0" ]] \
  || fail "a refused creation was recorded as having happened, in a table nothing can correct"
ok "a refused action writes no entry — an audit trail records what happened, and cannot be taken back"

# --- signing out, which is the least obvious of the three ------------------------------------------
#
# A trail with sign-ins and no sign-outs makes every session look open until it expired, which is
# wrong about exactly the accounts that were being careful.

status="$(curl -s -X DELETE -o "$WORKDIR/admin-audit-out.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $audit_made_token" -H "Idempotency-Key: verify-adm-audit-out-$$" \
  "http://localhost:$VERIFY_PORT/v1/admin/sessions/current")"
[[ "$status" == "204" ]] || { cat "$WORKDIR/admin-audit-out.json"; fail "signing out returned $status, want 204"; }

[[ "$(audit_count administrator.signed_out "$audit_made_id")" == "1" ]] \
  || fail "signing out wrote no audit entry, so every session looks open until it expired"
ok "an administrator signing out is recorded, so a session that was closed is distinguishable from one that lapsed"

# --- and what the service wrote is as immutable as what a psql prompt wrote ------------------------
#
# The SHIP-148 section above holds the trigger to account over an entry it inserted itself. This is
# the same control read from the other end: the rows the platform *actually writes* are inside the
# guarantee, which is the claim an operator cares about and is not quite the same statement.

service_entry="$("$PSQL" "$DATABASE_URL" -qtAc \
  "select id from audit_log
    where actor_id = '$audit_id' and action = 'administrator.created' limit 1;")"
[[ -n "$service_entry" ]] || fail "the entry the service wrote could not be found"

if "$PSQL" "$DATABASE_URL" -q -c \
  "update audit_log set reason = 'rewritten' where id = '$service_entry';" >/dev/null 2>&1; then
  fail "an entry this service wrote was rewritten"
fi
if "$PSQL" "$DATABASE_URL" -q -c \
  "delete from audit_log where id = '$service_entry';" >/dev/null 2>&1; then
  fail "an entry this service wrote was deleted"
fi
ok "and an entry the service wrote cannot be rewritten or deleted either — the invariant covers the rows that matter"

admin_clear_limits

# ==========================================================================================
# SHIP-151 — searching user accounts.
#
# # Three of the four terms, and the fourth said out loud
#
# The *Done when* is "search users by email, phone, name, and status". Email, phone and status are
# below. **Name has no column anywhere in the schema** — `users` never held one and registration
# never asks — so there is nothing to search and nothing to check. Docs/11 §4 records it. A check
# here asserting that a name search returns nothing would read as a passing test of a working
# feature, which is the opposite of recording a gap.
#
# # Every account is this run's own
#
# The `users` table is shared with every other section and every previous run, so a count would be a
# statement about the machine. Each search below is fenced on the `$$`-suffixed addresses this
# section registers, and the assertions are "this account is in the result" rather than "the result
# has N rows".

ticket "SHIP-151  an administrator searches accounts by email, phone and standing"

admin_clear_limits

# The 0419 prefix is this section's; 04190…04192 are the dispute fixtures above.
search_email="verify-search-$$@example.com"
search_phone="04193$$"
# The name is this section's own, and it is deliberately not an ordinary one: SHIP-30a's third
# clause is that a search term matches it, and a fixture called "Verify Harness" would be matched
# by every other section's fixture too.
search_name="Kirralee Wongabri"
status="$(post_json "verify-adm-search-reg-$$" /v1/auth/register \
  "{\"name\":\"$search_name\",\"email\":\"$search_email\",\"phone\":\"$search_phone\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/search-user.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/search-user.json"; fail "could not register the searchable customer: $status"; }
search_user_id="$(json "$WORKDIR/search-user.json" '["id"]')"

suspended_email="verify-search-gone-$$@example.com"
status="$(post_json "verify-adm-search-susp-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$suspended_email\",\"phone\":\"04194$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/search-suspended.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/search-suspended.json"; fail "could not register the suspended provider: $status"; }
suspended_user_id="$(json "$WORKDIR/search-suspended.json" '["id"]')"

"$PSQL" "$DATABASE_URL" -q -c \
  "update users set status = 'suspended' where id = '$suspended_user_id';" >/dev/null \
  || fail "the account could not be suspended"

# A support administrator, because looking is what the least-privileged role exists to be able to do
# and Docs/01 §4.6 lists searching first.
search_admin_email="verify-admin-search-$$@example.com"
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$search_admin_email', 'Verify Searcher', '$admin_fixture_hash', 'support');" >/dev/null \
  || fail "the searching administrator could not be created"

status="$(admin_signin "verify-adm-search-in-$$" "$search_admin_email" "$admin_password" search-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-search-in.json"; fail "the searching administrator could not sign in ($status)"; }
search_token="$(json "$WORKDIR/admin-search-in.json" '["token"]')"

# search_finds <query-string> <user-id> <name> — 1 when the account is on the page, 0 when it is not.
search_finds() {
  local found
  status="$(admin_get "/v1/admin/users?$1" "$search_token" "$3")"
  [[ "$status" == "200" ]] || { cat "$WORKDIR/admin-$3.json"; fail "searching accounts returned $status, want 200"; }
  found="$(python3 - "$WORKDIR/admin-$3.json" "$2" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
print(sum(1 for u in (page.get("data") or []) if u["id"] == sys.argv[2]))
PY
)"
  printf '%s' "$found"
}

[[ "$(search_finds "q=$search_email" "$search_user_id" search-email)" == "1" ]] \
  || fail "an account could not be found by its whole email address"
[[ "$(search_finds "q=verify-search-$$" "$search_user_id" search-partial)" == "1" ]] \
  || fail "an account could not be found by part of its email address"
ok "an account is found by its email address, whole or in part"

[[ "$(search_finds "q=$search_phone" "$search_user_id" search-phone)" == "1" ]] \
  || fail "an account could not be found by its phone number"
ok "and by its phone number, through the same parameter — support has a string and does not always know which it is"

# --- SHIP-30a: the fourth term, which had no column until registration collected one ------------
#
# This endpoint shipped serving three of its *Done when*'s four terms because `users` held no name.
# The clause SHIP-30a owes it is that all four now answer **on the wire**, so the account is
# registered through `POST /v1/auth/register` above rather than inserted, and found here through
# `GET /v1/admin/users` — the two halves of the ticket meeting at the only place they can.

# The space is percent-encoded rather than sent raw: this goes into a URL and curl would send a
# bare space as one, which is a malformed request line rather than a search for two words.
[[ "$(search_finds "q=Kirralee%20Wongabri" "$search_user_id" search-name)" == "1" ]] \
  || fail "an account could not be found by the whole name it registered with"
[[ "$(search_finds "q=Wongabri" "$search_user_id" search-name-part)" == "1" ]] \
  || fail "an account could not be found by part of its name"
[[ "$(search_finds "q=wongabri" "$search_user_id" search-name-case)" == "1" ]] \
  || fail "the name term is case-sensitive; a support engineer types what is on the ticket"
ok "an account is found by its name — whole, in part, and in any case (SHIP-30a)"

# The suspended provider registered as "Verify Harness" and the searchable customer did not, which
# makes this a real negative rather than one that would pass against an empty table.
[[ "$(search_finds "q=Kirralee%20Wongabri" "$suspended_user_id" search-name-neg)" == "0" ]] \
  || fail "a name term matched an account with a different name"
ok "and a name term matches that name rather than everybody"

# The name reaches the response too. A predicate that matched and a shape that did not carry it
# would leave a console unable to show what it had matched on.
status="$(admin_get "/v1/admin/users?q=$search_phone" "$search_token" search-name-shape)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-search-name-shape.json"; fail "searching returned $status"; }
[[ "$(json "$WORKDIR/admin-search-name-shape.json" '["data"][0]["name"]')" == "$search_name" ]] \
  || fail "the account came back without the name it registered with"
ok "and the name is on the response, as the account registered it"

# --- the standing filter, in both directions --------------------------------------------------------

[[ "$(search_finds "q=verify-search-$$&status=suspended" "$suspended_user_id" search-susp)" == "1" ]] \
  || fail "a suspended account is not found when filtering for suspended accounts"
[[ "$(search_finds "q=verify-search-$$&status=suspended" "$search_user_id" search-susp-neg)" == "0" ]] \
  || fail "an active account came back from a search filtered to suspended ones"
ok "the standing filter finds the accounts in that standing and only those"

# An unrecognised standing is refused rather than ignored. Ignoring it answers with every account,
# which reads exactly like "every account is suspended" to somebody who mistyped one.
status="$(admin_get "/v1/admin/users?status=suspeneded" "$search_token" search-bad-status)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/admin-search-bad-status.json"; fail "a mistyped standing returned $status, want 422"; }
[[ "$(cat "$WORKDIR/admin-search-bad-status.json")" == *'"status"'* ]] \
  || { cat "$WORKDIR/admin-search-bad-status.json"; fail "the refusal does not name the field"; }
ok "a standing the platform does not have is refused and names the field, rather than answering with everybody"

# --- a search term is a string, not a pattern ---------------------------------------------------
#
# The one defect here with a security shape. `%` is LIKE's "anything", so an unescaped term of `%`
# would return the whole table to the least-privileged role from one character in a search box.

[[ "$(search_finds "q=%25" "$search_user_id" search-wildcard)" == "0" ]] \
  || fail "a bare % matched every account, so the search term is being used as a LIKE pattern"
ok "a bare wildcard matches nothing — a search term is a string somebody typed, not a pattern"

# --- what the shape carries, and what it must never ---------------------------------------------

status="$(admin_get "/v1/admin/users?q=$search_email" "$search_token" search-shape)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-search-shape.json"; fail "searching accounts returned $status"; }

python3 - "$WORKDIR/admin-search-shape.json" >"$WORKDIR/search-keys.txt" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
items = page.get("data") or []
print(",".join(sorted(items[0].keys())) if items else "")
PY
search_keys="$(cat "$WORKDIR/search-keys.txt")"
[[ "$search_keys" == "created_at,email,email_verified_at,id,name,phone,phone_verified_at,role,status" ]] \
  || fail "the administrator's view of an account carries [$search_keys], which is not the closed set this shape is held to"
ok "an account carries a closed set of account facts — no jobs, no bids, no budget and no credential material"

status="$(admin_get "/v1/admin/users?q=$search_email" "" search-nocred)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/admin-search-nocred.json"; fail "the account search is readable without a credential ($status)"; }
[[ "$(cat "$WORKDIR/admin-search-nocred.json")" != *"$search_email"* ]] \
  || fail "a refused search returned accounts anyway"
ok "and the search is behind the administrator credential, like every other administrative route"

admin_clear_limits

# ==========================================================================================
# SHIP-152 — searching jobs, and opening one with its full bid and status history.
#
# # What only this section can show
#
# The statements behind `admin.JobDirectory` live in **cmd/api**, because they span `jobs`,
# `bids` and `job_status_history` — three tables belonging to two domains `internal/admin` may not
# import. The Go suite in that package drives the handler against a directory that records what it
# was asked for, which establishes the query translation and nothing about the SQL. **The SQL is
# demonstrated here or nowhere**, exactly as SHIP-113's party lookup and SHIP-117's exception queue
# are.
#
# # Every assertion is fenced on this run's own job
#
# `jobs` is shared with every other section, every previous run and every other worktree, so a count
# would be a statement about the machine. The job is found by the id this section created and the
# negative cases are asserted against a second job it also created.

ticket "SHIP-152  an administrator searches jobs and opens one with its bids and status history"

admin_clear_limits

# A delivered job with an accepted bid, which gives the detail view something in both lists: seven
# recorded transitions and one bid. dispute_delivered_job is the SHIP-163 fixture above and is
# reused rather than copied — it is already "a job moved through the guard the way 000402 demands".
console_job="$(dispute_delivered_job console)"
console_other="$(dispute_draft consoleother)"

# The goods description is what `q` matches, and no endpoint sets one on these fixtures — SHIP-63
# publishes and SHIP-71 collects the details, and the fixture writes the row directly. So it is set
# here, with a term unique to this run.
console_term="conso1e-$$-piano"
"$PSQL" "$DATABASE_URL" -q -c \
  "update jobs set goods_description = '$console_term and two stools' where id = '$console_job';" >/dev/null \
  || fail "the searchable goods description could not be set"

# A support administrator, because `jobs.read` is held by every role: looking is what the
# least-privileged one exists to be able to do, and Docs/01 §4.6 lists searching first.
console_email="verify-admin-console-$$@example.com"
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$console_email', 'Verify Console', '$admin_fixture_hash', 'support');" >/dev/null \
  || fail "the console reader could not be created"

status="$(admin_signin "verify-adm-console-in-$$" "$console_email" "$admin_password" console-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-console-in.json"; fail "the console reader could not sign in ($status)"; }
console_token="$(json "$WORKDIR/admin-console-in.json" '["token"]')"

# console_finds <query-string> <job-id> <name> — 1 when the job is on the page, 0 when it is not.
console_finds() {
  local found
  status="$(admin_get "/v1/admin/jobs?$1" "$console_token" "$3")"
  [[ "$status" == "200" ]] || { cat "$WORKDIR/admin-$3.json"; fail "searching jobs returned $status, want 200"; }
  found="$(python3 - "$WORKDIR/admin-$3.json" "$2" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
print(sum(1 for j in (page.get("data") or []) if j["id"] == sys.argv[2]))
PY
)"
  printf '%s' "$found"
}

[[ "$(console_finds "q=$console_term" "$console_job" console-term)" == "1" ]] \
  || fail "a job could not be found by part of its goods description"
ok "a job is found by part of its goods description"

[[ "$(console_finds "q=$console_term&status=Delivered" "$console_job" console-status)" == "1" ]] \
  || fail "a Delivered job is not found when filtering for Delivered jobs"
[[ "$(console_finds "q=$console_term&status=Open" "$console_job" console-status-neg)" == "0" ]] \
  || fail "a Delivered job came back from a search filtered to Open ones"
ok "the status filter takes Docs/02 §1's stored form and finds the jobs in that status and only those"

[[ "$(console_finds "customer=$dispute_customer_id&status=Delivered" "$console_job" console-customer)" == "1" ]] \
  || fail "a job could not be found by its customer"
[[ "$(console_finds "customer=$dispute_provider_id" "$console_job" console-customer-neg)" == "0" ]] \
  || fail "a job came back from a search filtered to an account that does not own it"
ok "and the customer filter narrows to one account's jobs"

# An unrecognised status is refused rather than ignored, for the reason the account search refuses
# an unrecognised standing: ignoring it answers with every job, which reads exactly like "the whole
# marketplace is delivered" to somebody who mistyped one. 422, because it is a field-level
# validation failure in validate.Errors' shape and not a malformed request.
status="$(admin_get "/v1/admin/jobs?status=Delivred" "$console_token" console-bad-status)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/admin-console-bad-status.json"; fail "a mistyped job status returned $status, want 422"; }
[[ "$(cat "$WORKDIR/admin-console-bad-status.json")" == *'"status"'* ]] \
  || { cat "$WORKDIR/admin-console-bad-status.json"; fail "the refusal does not name the field"; }
[[ "$(cat "$WORKDIR/admin-console-bad-status.json")" == *'En route to pickup'* ]] \
  || { cat "$WORKDIR/admin-console-bad-status.json"; fail "the refusal does not say what the statuses are"; }
ok "a status the platform does not have is refused, names the field and lists the twelve — rather than answering with every job"

status="$(admin_get "/v1/admin/jobs?customer=not-a-uuid" "$console_token" console-bad-customer)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/admin-console-bad-customer.json"; fail "a malformed customer filter returned $status, want 422"; }
ok "and so is a customer filter that is not an identifier"

# --- a search term is a string, not a pattern ---------------------------------------------------
#
# The one defect here with a security shape, and the same one the account search carries. `%` is
# LIKE's "anything", so an unescaped term of `%` returns every job in the marketplace to the
# least-privileged role from one character in a search box.

[[ "$(console_finds "q=%25" "$console_job" console-wildcard)" == "0" ]] \
  || fail "a bare % matched every job, so the search term is being used as a LIKE pattern"
ok "a bare wildcard matches nothing — a search term is a string somebody typed, not a pattern"

# --- opening the job, which is the half the Done when is really about ----------------------------

status="$(admin_get "/v1/admin/jobs/$console_job" "$console_token" console-open)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-console-open.json"; fail "opening a job returned $status, want 200"; }

[[ "$(json "$WORKDIR/admin-console-open.json" '["job"]["id"]')" == "$console_job" ]] \
  || { cat "$WORKDIR/admin-console-open.json"; fail "the detail view answered with the wrong job"; }
[[ "$(json "$WORKDIR/admin-console-open.json" '["job"]["status"]')" == "Delivered" ]] \
  || { cat "$WORKDIR/admin-console-open.json"; fail "the job's status is not the stored form the console filters on"; }
ok "any job opens — there is no ownership scope on this endpoint, which is what Docs/01 §4.6 asks for"

python3 - "$WORKDIR/admin-console-open.json" "$dispute_provider_id" >"$WORKDIR/console-detail.txt" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
bids, history = d["bids"], d["history"]
accepted = [b for b in bids if b["status"] == "Accepted" and b["provider_id"] == sys.argv[2]]
print(len(bids))
print(len(history))
print(accepted[0]["amount_cents"] if accepted else "")
print(",".join(sorted(bids[0].keys())) if bids else "")
print(",".join(sorted(history[0].keys())) if history else "")
print(",".join(sorted(d["job"].keys())))
print(history[0]["from"] + ">" + history[0]["to"] if history else "")
print(history[-1]["to"] if history else "")
PY
console_bids="$(sed -n '1p' "$WORKDIR/console-detail.txt")"
console_history="$(sed -n '2p' "$WORKDIR/console-detail.txt")"
console_amount="$(sed -n '3p' "$WORKDIR/console-detail.txt")"

[[ "$console_bids" == "1" ]] \
  || { cat "$WORKDIR/admin-console-open.json"; fail "the detail view carries $console_bids bids, want the one that was accepted"; }
[[ "$console_amount" == "45000" ]] \
  || { cat "$WORKDIR/admin-console-open.json"; fail "the accepted bid's amount is $console_amount cents, want 45000 — numeric(12,2) converted in SQL"; }
ok "the full bid history is on the job, with the amount in cents — Docs/02 §4's third reader, which had no route until there was an administrator"

# Six transitions, because dispute_delivered_job makes six *moves*: Draft → Open → Awarded → En
# route to pickup → Picked up → In transit → Delivered. Seven statuses, six rows — a job_status_history
# row records a move rather than a state, and the Draft the job started in was never moved *into*.
# Asserted as a count *and* by both ends, so a statement returning one row per join rather than one
# per transition fails here.
[[ "$console_history" == "6" ]] \
  || { cat "$WORKDIR/admin-console-open.json"; fail "the detail view carries $console_history transitions, want 6"; }
[[ "$(sed -n '7p' "$WORKDIR/console-detail.txt")" == "Draft>Open" ]] \
  || { cat "$WORKDIR/admin-console-open.json"; fail "the history does not start where the job did"; }
[[ "$(sed -n '8p' "$WORKDIR/console-detail.txt")" == "Delivered" ]] \
  || { cat "$WORKDIR/admin-console-open.json"; fail "the history is not ordered oldest first on the platform's clock"; }
ok "and the full status history, oldest first on the platform's clock rather than the device's (Docs/02 §3.1)"

# --- the closed key sets, which are where a budget would arrive ----------------------------------
#
# Docs/01 §4.3's invariant names providers, so an administrator is not the audience it protects the
# customer's maximum from — leaving it out is a decision (see internal/admin/jobsearch.go) and the
# way it is kept is a closed key set rather than a search for the word "budget". SHIP-83 established
# the axis: a field called `max_price` passes a spelling check and leaks the same fact.

console_job_keys="$(sed -n '6p' "$WORKDIR/console-detail.txt")"
[[ "$console_job_keys" == "bid_count,created_at,customer_id,expires_at,goods_description,id,status,updated_at" ]] \
  || fail "the administrator's view of a job carries [$console_job_keys], which is not the closed set this shape is held to"

console_bid_keys="$(sed -n '4p' "$WORKDIR/console-detail.txt")"
[[ "$console_bid_keys" == "amount_cents,created_at,deliver_by,id,message,offered_by,pickup_at,provider_id,status,superseded_by,updated_at" ]] \
  || fail "the administrator's view of a bid carries [$console_bid_keys], which is not the closed set this shape is held to"

console_event_keys="$(sed -n '5p' "$WORKDIR/console-detail.txt")"
[[ "$console_event_keys" == "actor_id,actor_recorded_at,actor_type,from,id,reason,server_recorded_at,to" ]] \
  || fail "the administrator's view of a transition carries [$console_event_keys], which is not the closed set this shape is held to"
ok "the job, the bid and the transition each carry a closed set of keys — no budget, no address and no contact detail on any of the three"

# --- what a missing job answers, and what an unauthenticated caller does --------------------------

status="$(admin_get "/v1/admin/jobs/$(uuidgen 2>/dev/null || python3 -c 'import uuid;print(uuid.uuid4())')" "$console_token" console-missing)"
[[ "$status" == "404" ]] \
  || { cat "$WORKDIR/admin-console-missing.json"; fail "opening a job that does not exist returned $status, want 404"; }
ok "a job that does not exist is a 404 — and unlike the same answer on dispute intake, nothing is being kept from the caller"

status="$(admin_get "/v1/admin/jobs?q=$console_term" "" console-nocred)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/admin-console-nocred.json"; fail "the job search is readable without a credential ($status)"; }
[[ "$(cat "$WORKDIR/admin-console-nocred.json")" != *"$console_term"* ]] \
  || fail "a refused search returned jobs anyway"

status="$(admin_get "/v1/admin/jobs/$console_job" "" console-open-nocred)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/admin-console-open-nocred.json"; fail "a job is openable without a credential ($status)"; }
ok "both routes are behind the administrator credential, like every other administrative route"

# The other fixture exists so the search is a filter rather than a passthrough: a job with no
# description never matches a term, which is the right answer because it has none to match.
[[ "$(console_finds "q=$console_term" "$console_other" console-other)" == "0" ]] \
  || fail "a job with no goods description matched a search term"
ok "a draft with no goods description matches no term — NULL ILIKE is NULL, not a match on everything"

admin_clear_limits

# ==========================================================================================
# SHIP-165 — the immutable history, searchable by actor, target and date.
#
# # What only this section can show
#
# The Go suite drives the reader against a real database and is the stronger of the two — it writes
# entries through the real [Auditor] and reads them back through the real statement. **What it cannot
# show is that the wiring in cmd/api hands the handler a trail at all**: `adminHandler` builds one
# from `d.Pool` and passes it in the services struct, and a change that passed a nil, or that built
# the reader against a different pool, compiles and passes every Go test in the domain.
#
# It also shows the half that matters most operationally: the entries this **running service** wrote
# for its own administrative actions are the ones the endpoint returns. A reader over a table nothing
# populates would pass every filter test ever written.
#
# # Everything is fenced on this run's own administrator
#
# `audit_log` holds every previous run's entries and every other worktree's, so a count over the
# table would be a statement about the machine. Every assertion below is on entries whose actor is an
# account this section created.

ticket "SHIP-165  the audit trail is searchable by actor, target and date"

admin_clear_limits

# An owner, because this section needs to *cause* audit entries as well as read them — and creating
# an administrator is the one action that needs `admins.manage`. Reading needs only `audit.read`,
# which the support administrator below is used to prove.
trail_email="verify-admin-trail-$$@example.com"
trail_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$trail_email', 'Verify Trail', '$admin_fixture_hash', 'owner')
   returning id;")"
[[ -n "$trail_id" ]] || fail "the trail fixture administrator could not be created"

status="$(admin_signin "verify-adm-trail-in-$$" "$trail_email" "$admin_password" trail-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-trail-in.json"; fail "the trail fixture could not sign in ($status)"; }
trail_token="$(json "$WORKDIR/admin-trail-in.json" '["token"]')"

# One administrator created, which is the entry with a target and metadata worth reading back.
trail_made_email="verify-admin-trail-made-$$@example.com"
status="$(curl -s -X POST -o "$WORKDIR/admin-trail-made.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $trail_token" -H "Idempotency-Key: verify-adm-trail-made-$$" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$trail_made_email\",\"name\":\"Trail Made\",\"password\":\"$admin_password\",\"role\":\"moderator\"}" \
  "http://localhost:$VERIFY_PORT/v1/admin/administrators")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/admin-trail-made.json"; fail "creating an administrator returned $status, want 201"; }
trail_made_id="$(json "$WORKDIR/admin-trail-made.json" '["id"]')"

# A support administrator does the reading, because `audit.read` is held by every role: a trail only
# the people it records can read is not a control.
trail_reader_email="verify-admin-trail-reader-$$@example.com"
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$trail_reader_email', 'Verify Reader', '$admin_fixture_hash', 'support');" >/dev/null \
  || fail "the trail reader could not be created"

status="$(admin_signin "verify-adm-trail-read-$$" "$trail_reader_email" "$admin_password" trail-read)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-trail-read.json"; fail "the trail reader could not sign in ($status)"; }
trail_reader_token="$(json "$WORKDIR/admin-trail-read.json" '["token"]')"

# trail_actions <query-string> <name> — the actions on the page, comma separated, deduplicated.
trail_actions() {
  status="$(admin_get "/v1/admin/audit?$1" "$trail_reader_token" "$2")"
  [[ "$status" == "200" ]] || { cat "$WORKDIR/admin-$2.json"; fail "reading the audit trail returned $status, want 200"; }
  python3 - "$WORKDIR/admin-$2.json" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
print(",".join(sorted({e["action"] for e in (page.get("data") or [])})))
PY
}

# --- by actor, which is the first question anybody asks of a trail --------------------------------

trail_seen="$(trail_actions "actor=$trail_id&limit=50" trail-actor)"
[[ "$trail_seen" == "administrator.created,administrator.signed_in" ]] \
  || { cat "$WORKDIR/admin-trail-actor.json"; fail "a search by actor returned [$trail_seen], want this run's sign-in and creation and nothing else"; }
ok "the trail is searchable by actor, and returns exactly what that administrator did — including the entries this running service wrote for its own actions"

# The negative direction, which is the half a filter that does nothing would still pass. The reader
# has signed in and so has an entry of its own; it must not appear under another actor.
python3 - "$WORKDIR/admin-trail-actor.json" "$trail_id" >"$WORKDIR/trail-foreign.txt" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
print(sum(1 for e in (page.get("data") or []) if e["actor_id"] != sys.argv[2]))
PY
[[ "$(cat "$WORKDIR/trail-foreign.txt")" == "0" ]] \
  || { cat "$WORKDIR/admin-trail-actor.json"; fail "a search by actor returned another administrator's entries, so the filter does nothing"; }
ok "and only that administrator's — the other accounts this run signed in are not on the page"

# --- by target ------------------------------------------------------------------------------------

trail_seen="$(trail_actions "target=$trail_made_id&limit=50" trail-target)"
[[ "$trail_seen" == "administrator.created" ]] \
  || { cat "$WORKDIR/admin-trail-target.json"; fail "a search by target returned [$trail_seen], want the creation of that account alone"; }
ok "and by target, which answers 'everything that ever happened to this' without the caller knowing what kind of thing it is"

# --- by action, and by date -----------------------------------------------------------------------

trail_seen="$(trail_actions "actor=$trail_id&action=administrator.created&limit=50" trail-action)"
[[ "$trail_seen" == "administrator.created" ]] \
  || { cat "$WORKDIR/admin-trail-action.json"; fail "a search by action returned [$trail_seen]"; }
ok "and by action — the column 000003 made a stable identifier rather than a sentence, so that this search exists"

# Today, as a bare day. A support engineer types a date and a console sends an instant, and both are
# accepted; `from` is inclusive and `to` is exclusive so consecutive days tile without overlapping.
trail_today="$("$PSQL" "$DATABASE_URL" -qtAc "select to_char(now() at time zone 'utc', 'YYYY-MM-DD');")"
trail_tomorrow="$("$PSQL" "$DATABASE_URL" -qtAc "select to_char((now() at time zone 'utc') + interval '1 day', 'YYYY-MM-DD');")"

trail_seen="$(trail_actions "actor=$trail_id&from=$trail_today&to=$trail_tomorrow&limit=50" trail-today)"
[[ "$trail_seen" == "administrator.created,administrator.signed_in" ]] \
  || { cat "$WORKDIR/admin-trail-today.json"; fail "a search bounded to today returned [$trail_seen]"; }

trail_seen="$(trail_actions "actor=$trail_id&from=$trail_tomorrow&limit=50" trail-tomorrow)"
[[ -z "$trail_seen" ]] \
  || { cat "$WORKDIR/admin-trail-tomorrow.json"; fail "a search starting tomorrow returned [$trail_seen], so the lower bound does nothing"; }
ok "and by date, as a bare day — the bounds are half-open, so nothing written today falls inside a window that starts tomorrow"

# --- what an entry carries, and that it is exactly what was written --------------------------------

status="$(admin_get "/v1/admin/audit?target=$trail_made_id" "$trail_reader_token" trail-entry)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-trail-entry.json"; fail "reading the audit trail returned $status"; }

python3 - "$WORKDIR/admin-trail-entry.json" "$trail_id" "$trail_made_id" >"$WORKDIR/trail-entry.txt" <<'PY'
import json, sys
e = (json.load(open(sys.argv[1])).get("data") or [None])[0]
if e is None:
    print("MISSING"); raise SystemExit
print(",".join(sorted(e.keys())))
print(e["actor_type"])
print("actor-ok" if e["actor_id"] == sys.argv[2] else "actor-" + str(e["actor_id"]))
print("target-ok" if e["target_id"] == sys.argv[3] else "target-" + str(e["target_id"]))
print(e["target_type"])
# The role granted, which is the field an entry about a created administrator most needs — a
# record that an account was made without saying what it may do is half a fact. Asserted by key
# rather than as the whole object, because the writer also records the address and a whole-object
# comparison would make this section fail the next time somebody adds a field to the metadata.
print(str(e["metadata"].get("role")))
print("object" if isinstance(e["metadata"], dict) else "not-an-object")
PY
trail_keys="$(sed -n '1p' "$WORKDIR/trail-entry.txt")"
[[ "$trail_keys" == "action,actor_id,actor_type,created_at,id,metadata,reason,target_id,target_type" ]] \
  || { cat "$WORKDIR/admin-trail-entry.json"; fail "an audit entry carries [$trail_keys], which is not every column of the row"; }
[[ "$(sed -n '2p' "$WORKDIR/trail-entry.txt")" == "admin" ]] || fail "the entry does not say an administrator acted"
[[ "$(sed -n '3p' "$WORKDIR/trail-entry.txt")" == "actor-ok" ]] || fail "the entry does not name the administrator who acted"
[[ "$(sed -n '4p' "$WORKDIR/trail-entry.txt")" == "target-ok" ]] || fail "the entry does not name the account that was created"
[[ "$(sed -n '5p' "$WORKDIR/trail-entry.txt")" == "administrator" ]] || fail "the entry does not say what kind of thing it names"
[[ "$(sed -n '6p' "$WORKDIR/trail-entry.txt")" == "moderator" ]] \
  || { cat "$WORKDIR/admin-trail-entry.json"; fail "the metadata does not carry the role that was granted: $(sed -n '6p' "$WORKDIR/trail-entry.txt")"; }
[[ "$(sed -n '7p' "$WORKDIR/trail-entry.txt")" == "object" ]] \
  || { cat "$WORKDIR/admin-trail-entry.json"; fail "the metadata is not a JSON object; null is what a console renders by crashing"; }
ok "an entry carries every column of the row, including the metadata as the object that was written — a viewer that redacted would be a second record"

# --- the filters are refused rather than ignored ---------------------------------------------------
#
# The direction that matters most for this endpoint. An ignored filter answers with the whole trail,
# and a support engineer who mistyped an action would read "no entries" as "it never happened".

status="$(admin_get "/v1/admin/audit?action=administrator.deleted" "$trail_reader_token" trail-bad-action)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/admin-trail-bad-action.json"; fail "an action nothing records returned $status, want 422"; }
[[ "$(cat "$WORKDIR/admin-trail-bad-action.json")" == *'"action"'* ]] \
  || { cat "$WORKDIR/admin-trail-bad-action.json"; fail "the refusal does not name the field"; }

status="$(admin_get "/v1/admin/audit?from=last%20Tuesday" "$trail_reader_token" trail-bad-date)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/admin-trail-bad-date.json"; fail "a date that is not a date returned $status, want 422"; }

status="$(admin_get "/v1/admin/audit?from=$trail_tomorrow&to=$trail_today" "$trail_reader_token" trail-bad-range)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/admin-trail-bad-range.json"; fail "an inverted range returned $status, want 422"; }
ok "an action nothing records, a date that is not a date and a range that ends before it starts are each refused and name their field"

# --- the invariant this ticket is most able to break -----------------------------------------------
#
# CLAUDE.md: audit entries are append-only and ordinary administrators cannot delete them. SHIP-165
# is the ticket that gives the trail a reader, and the obvious next thing somebody adds is a way to
# correct one. There is no such route, and the four verbs are refused on the path that exists.

for verb in POST PUT PATCH DELETE; do
  status="$(curl -s -X "$verb" -o "$WORKDIR/admin-trail-$verb.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $trail_reader_token" -H "Idempotency-Key: verify-adm-trail-$verb-$$" \
    -H 'Content-Type: application/json' -d '{}' \
    "http://localhost:$VERIFY_PORT/v1/admin/audit")"
  [[ "$status" == "404" || "$status" == "405" ]] \
    || { cat "$WORKDIR/admin-trail-$verb.json"; fail "$verb on the audit trail returned $status; the trail is append-only and no route may change it"; }
done
ok "no verb but GET is served on the trail — entries are written by the actions that cause them, in the transaction that performs them, and never by a request"

status="$(admin_get "/v1/admin/audit" "" trail-nocred)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/admin-trail-nocred.json"; fail "the audit trail is readable without a credential ($status)"; }
[[ "$(cat "$WORKDIR/admin-trail-nocred.json")" != *"$trail_id"* ]] \
  || fail "a refused request returned entries anyway"
ok "and the trail is behind the administrator credential — without one it would be a public list of who runs the platform and what they touch"

admin_clear_limits

# ==========================================================================================
# SHIP-160 — unpublishing a policy-breaching job, and SHIP-161 — restricting or suspending an
# account.
#
# # What only this section can show
#
# Two things, and the second is the one the Go suite structurally cannot reach.
#
# The composition root's half: `adminHandler` builds the enforcement service from the same
# `disputeLifecycle` adapter the dispute workflow uses, and `disputeLifecycle.Unpublish` is the
# translation between `jobs`' sentinels and `admin.JobMove`. `internal/admin`'s tests use their own
# copy of that adapter, so a change that wired a different lifecycle, or that mistranslated
# `ErrTransitionNotPermitted`, compiles and passes every Go test in the domain.
#
# **And SHIP-161's enforcement, which lives in `internal/identity`.** The *Done when* is "account
# access is limited or disabled", and no test in `internal/admin` can show that: the domain writes a
# column, and whether a suspended account can still sign in is a question for a different domain's
# code, through the real HTTP surface, against the real running service. That is demonstrated here
# or nowhere.
#
# # Everything is fenced on rows this run created
#
# `jobs`, `users` and `audit_log` are shared with every previous run and every other worktree, so
# every assertion below is on identifiers this section made.

# **Every key below is prefixed `verify-adm160-` or `verify-adm161-`, and that is not cosmetic.**
# `dispute_draft <name>` sends `verify-adm-<name>-$$` and `admin_signin <key>` sends the key it is
# handed, so a section reusing one string across two different bodies is answered
# `idempotency_key_reused` by the middleware — a 409 where the check expects a 403, which reads as a
# broken endpoint rather than as two requests sharing a key. This section paid for that once.

ticket "SHIP-160  an administrator unpublishes a policy-breaching job, with a recorded reason"

admin_clear_limits

# A moderator, because `jobs.unpublish` is held by `moderator` and `owner` and not by `support` —
# the split that makes Docs/04 §9's least-privilege control real rather than nominal.
unpub_email="verify-admin-unpub-$$@example.com"
unpub_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$unpub_email', 'Verify Moderator', '$admin_fixture_hash', 'moderator')
   returning id;")"
[[ -n "$unpub_id" ]] || fail "the unpublishing moderator could not be created"

status="$(admin_signin "verify-adm160-signin-$$" "$unpub_email" "$admin_password" unpub-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-unpub-in.json"; fail "the moderator could not sign in ($status)"; }
unpub_token="$(json "$WORKDIR/admin-unpub-in.json" '["token"]')"

# A support administrator too, for the permission check below.
unpub_support_email="verify-admin-unpub-sup-$$@example.com"
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$unpub_support_email', 'Verify Support', '$admin_fixture_hash', 'support');" >/dev/null \
  || fail "the support administrator could not be created"
status="$(admin_signin "verify-adm160-supsignin-$$" "$unpub_support_email" "$admin_password" unpub-sup)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-unpub-sup.json"; fail "the support administrator could not sign in ($status)"; }
unpub_support_token="$(json "$WORKDIR/admin-unpub-sup.json" '["token"]')"

# unpublish <token> <key> <job-id> <body> <name> — one attempt, answering with its status.
unpublish() {
  curl -s -X POST -o "$WORKDIR/unpub-$5.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$4" \
    "http://localhost:$VERIFY_PORT/v1/admin/jobs/$3/unpublish"
}

unpub_reason="Listing offers to transport a live animal, which Docs 05 prohibits."
unpub_body="{\"reason\":\"$unpub_reason\"}"

# An Open job: published, nobody committed. dispute_draft creates it and one guarded move publishes.
unpub_job="$(dispute_draft ship160job)"
dispute_move "$unpub_job" Draft Open
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$unpub_job';")" == "Open" ]] \
  || fail "the fixture job is not Open, so nothing below is testing what it claims"

# --- the permission, before anything succeeds -----------------------------------------------------

status="$(unpublish "$unpub_support_token" "verify-adm160-suptry-$$" "$unpub_job" "$unpub_body" sup)"
[[ "$status" == "403" ]] \
  || { cat "$WORKDIR/unpub-sup.json"; fail "a support administrator unpublished a job and got $status, want 403"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$unpub_job';")" == "Open" ]] \
  || fail "a refused removal moved the job anyway"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc "select count(*) from audit_log where target_id = '$unpub_job';")" == "0" ]] \
  || fail "a refused removal wrote an audit entry, in a table nothing can correct"
ok "a support administrator may open any job and may not remove one — two permissions on two endpoints, which is what makes least privilege real"

# --- a reason that records nothing is refused -----------------------------------------------------

status="$(unpublish "$unpub_token" "verify-adm160-noreason-$$" "$unpub_job" '{"reason":""}' noreason)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/unpub-noreason.json"; fail "an empty reason returned $status, want 422"; }
[[ "$(cat "$WORKDIR/unpub-noreason.json")" == *'"reason"'* ]] \
  || { cat "$WORKDIR/unpub-noreason.json"; fail "the refusal does not name the field"; }

status="$(unpublish "$unpub_token" "verify-adm160-short-$$" "$unpub_job" '{"reason":"spam"}' short)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/unpub-short.json"; fail "a four-character reason returned $status, want 422"; }
ok "an empty reason and one too short to record anything are both refused and name the field — a required field satisfied by one character is not a recorded reason"

# --- the removal itself ----------------------------------------------------------------------------

status="$(unpublish "$unpub_token" "verify-adm160-remove-$$" "$unpub_job" "$unpub_body" ok)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/unpub-ok.json"; fail "unpublishing returned $status, want 200"; }
[[ "$(json "$WORKDIR/unpub-ok.json" '["status"]')" == "Cancelled" ]] \
  || { cat "$WORKDIR/unpub-ok.json"; fail "the response does not say the job was cancelled"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$unpub_job';")" == "Cancelled" ]] \
  || fail "the job is not Cancelled"
ok "a published job is removed by moving it to Cancelled through the one guarded transition — there is no unpublished status and no hidden flag"

# The transition went through the guard, attributed to an administrator, with the reason. A bare
# UPDATE would have been refused by 000402, so reaching Cancelled at all is half the claim; that it
# is recorded against an *admin* rather than the customer is the other half.
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from job_status_history
    where job_id = '$unpub_job' and to_status = 'Cancelled'
      and actor_type = 'admin' and actor_id = '$unpub_id'
      and reason = '$unpub_reason';")" == "1" ]] \
  || fail "the transition is not recorded against the administrator with the reason they gave"
ok "and the job's status history names the administrator and says why — which is what a customer's support conversation reads"

[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log
    where target_id = '$unpub_job' and target_type = 'job'
      and action = 'job.unpublished' and actor_type = 'admin'
      and actor_id = '$unpub_id' and reason = '$unpub_reason';")" == "1" ]] \
  || fail "the removal wrote no audit entry naming the job, the administrator and the reason"
ok "and the audit log records the same reason against the administrator — two tables, two readers, and neither has to find the other"

# --- the customer is notified, and no new event arranges it ------------------------------------------
#
# The last clause of the *Done when*. The guarded transition emits `job.status_changed` in the same
# transaction and `notifications.StatusRules` routes a move to Cancelled to the job's customer and
# the awarded provider. **No `admin.job_unpublished` event was added**, which is the finding rather
# than a shortcut: a second announcement of one state change is the failure those rules exist to
# prevent. Fenced on this job's aggregate id rather than on a count or a timestamp, because the
# outbox is shared with every other worktree.

[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from outbox
    where aggregate_id = '$unpub_job' and event_type = 'job.status_changed'
      and payload->>'to' = 'Cancelled' and payload->>'actor_type' = 'admin';")" == "1" ]] \
  || fail "the removal emitted no job.status_changed to Cancelled, so nothing tells the customer"
ok "the removal emits job.status_changed to Cancelled — which notifications already routes to the customer, so the customer is notified without a second event about one state change"

# --- a second attempt, and a job past the point ------------------------------------------------------

status="$(unpublish "$unpub_token" "verify-adm160-again-$$" "$unpub_job" "$unpub_body" again)"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/unpub-again.json"; fail "unpublishing an already-removed job returned $status, want 409"; }
[[ "$(cat "$WORKDIR/unpub-again.json")" == *'admin_job_already_unpublished'* ]] \
  || { cat "$WORKDIR/unpub-again.json"; fail "the refusal does not say the job was already removed"; }
[[ "$("$PSQL" "$DATABASE_URL" -qtAc "select count(*) from audit_log where target_id = '$unpub_job';")" == "1" ]] \
  || fail "a second attempt wrote a second entry, so the trail says the job was taken down twice"
ok "a job somebody has already removed answers with its own code and writes nothing — the ordinary outcome of two moderators reading one queue"

# An awarded job. Docs/02 §2 offers no route from Awarded to Cancelled: a provider has committed,
# and Docs/02 §6.2 makes ending it a support case rather than a status change.
unpub_awarded="$(dispute_draft ship160awarded)"
dispute_move "$unpub_awarded" Draft Open
dispute_move "$unpub_awarded" Open Awarded
dispute_award "$unpub_awarded" "$dispute_provider_id"

status="$(unpublish "$unpub_token" "verify-adm160-awarded-$$" "$unpub_awarded" "$unpub_body" awarded)"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/unpub-awarded.json"; fail "unpublishing an awarded job returned $status, want 409"; }
[[ "$(cat "$WORKDIR/unpub-awarded.json")" == *'admin_job_not_unpublishable'* ]] \
  || { cat "$WORKDIR/unpub-awarded.json"; fail "the refusal does not carry the code a console branches on"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$unpub_awarded';")" == "Awarded" ]] \
  || fail "a refused removal moved an awarded job"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc "select count(*) from audit_log where target_id = '$unpub_awarded';")" == "0" ]] \
  || fail "a refused removal wrote an audit entry"
ok "an awarded job cannot be unpublished — Docs/02 §2 has no such row, and the administrator's path is a dispute they then resolve"

status="$(curl -s -X POST -o "$WORKDIR/unpub-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $unpub_token" -H 'Content-Type: application/json' -d "$unpub_body" \
  "http://localhost:$VERIFY_PORT/v1/admin/jobs/$unpub_job/unpublish")"
[[ "$status" == "400" ]] \
  || { cat "$WORKDIR/unpub-nokey.json"; fail "unpublishing without an Idempotency-Key returned $status, want 400"; }
ok "and it is refused without an Idempotency-Key — a retry after a dropped connection must not write a second entry into a table nothing can tidy"

admin_clear_limits

# ==========================================================================================
ticket "SHIP-161  an administrator restricts or suspends an account, and the account loses access"

admin_clear_limits

# **This is the one section in this file that signs a *user* in**, and SHIP-47 limits failed
# sign-ins per account and per network address — every request in `make verify` arrives from
# 127.0.0.1, so one address bucket is shared with 40-identity.sh, with every other section and with
# every previous run. The deliberate 403 below counts against it. Cleared here for the reason
# 40-identity.sh clears it at the point sign-ins begin: a run that ended part-way through would
# otherwise leave the bucket full and the *next* run would fail with a 429 that looks like a broken
# endpoint.
#
# `admin_clear_limits` above clears the administrator keyspace, which is a separate one — an
# administrator's failures and a user's against one address are separate allowances (see
# credentials.go). This clears the user one, in both of its namespaces: the account half is
# `signin:account:` and the address half is `credential:address:`, which is shared by sign-in,
# refresh and both verification endpoints since SHIP-183b.
for pattern in 'rl:v1:signin:*' 'rl:v1:credential:*'; do
  redis-cli -u "$REDIS_URL" --scan --pattern "$pattern" \
    | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true
done

# standing <token> <key> <user-id> <body> <name> — one attempt, answering with its status.
standing() {
  curl -s -X POST -o "$WORKDIR/stand-$5.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$4" \
    "http://localhost:$VERIFY_PORT/v1/admin/users/$3/standing"
}

# A real registered account, because the second half of this ticket is what happens when it tries to
# use the platform — and that needs a password the sign-in endpoint will accept.
stand_email="verify-standing-$$@example.com"
stand_password="correct-horse-battery-staple"
status="$(post_json "verify-adm161-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$stand_email\",\"phone\":\"04195$$\",\"password\":\"$stand_password\",\"role\":\"provider\"}" \
  "$WORKDIR/standing-user.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/standing-user.json"; fail "could not register the account: $status"; }
stand_user_id="$(json "$WORKDIR/standing-user.json" '["id"]')"

# --- the permission ---------------------------------------------------------------------------------

# `restricted` rather than `suspended` throughout this section, and the change is SHIP-166's.
#
# Docs/04 §9 took permanent suspension off this endpoint: one administrator may no longer do it
# alone. So what this section demonstrates is the **limited** half of the *Done when*'s "limited or
# disabled" — and the disabled half, including that a suspended account can neither sign in nor
# refresh, is demonstrated in the SHIP-166 section above, through the two-person route that is now
# the only way to reach it.
stand_reason="Two unresolved no-shows in a fortnight; see the delivery exception queue."
stand_body="{\"standing\":\"restricted\",\"reason\":\"$stand_reason\"}"

status="$(standing "$unpub_support_token" "verify-adm161-sup-$$" "$stand_user_id" "$stand_body" sup)"
[[ "$status" == "403" ]] \
  || { cat "$WORKDIR/stand-sup.json"; fail "a support administrator suspended an account and got $status, want 403"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from users where id = '$stand_user_id';")" == "active" ]] \
  || fail "a refused change altered the account anyway"
ok "a support administrator may search accounts and may not restrict one — the other half of the same least-privilege split"

# --- a standing the platform does not have, and a reason that records nothing -------------------------

status="$(standing "$unpub_token" "verify-adm161-bad-$$" "$stand_user_id" \
  '{"standing":"banned","reason":"Repeated policy breaches on delivery."}' bad)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/stand-bad.json"; fail "a standing the platform does not have returned $status, want 422"; }
[[ "$(cat "$WORKDIR/stand-bad.json")" == *'"standing"'* ]] \
  || { cat "$WORKDIR/stand-bad.json"; fail "the refusal does not name the field"; }

status="$(standing "$unpub_token" "verify-adm161-noreason-$$" "$stand_user_id" \
  '{"standing":"restricted","reason":"bad"}' noreason)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/stand-noreason.json"; fail "a reason that records nothing returned $status, want 422"; }
ok "a standing outside ck_users_status's three and a reason too short to record anything are both refused and name their field"

# --- the account is limited rather than disabled ------------------------------------------------------
#
# **This is the pair no Go test in internal/admin can make.** The domain writes a column; what that
# column *does* is internal/identity's code, and the two only meet in the running service.
#
# The distinction is the whole of Docs/04 §4's vocabulary and it is easy to lose: a **restricted**
# account may still sign in and read — `User.CanSignIn` refuses `suspended` alone — and may not
# trade. An implementation that refused a restricted account at sign-in would pass a check that only
# looked for "access is limited", and would lock somebody out of the messages telling them why.

status="$(post_json "verify-adm161-login1-$$" /v1/auth/login \
  "{\"email\":\"$stand_email\",\"password\":\"$stand_password\",\"device_label\":\"Verify Standing\"}" "$WORKDIR/stand-login-before.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/stand-login-before.json"; fail "the account could not sign in before being restricted ($status)"; }
ok "the account signs in while it is active, which is the precondition the next check needs"

status="$(standing "$unpub_token" "verify-adm161-limit-$$" "$stand_user_id" "$stand_body" susp)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/stand-susp.json"; fail "restricting returned $status, want 200"; }
[[ "$(json "$WORKDIR/stand-susp.json" '["from"]')" == "active" ]] \
  || { cat "$WORKDIR/stand-susp.json"; fail "the response does not say what the account held before"; }
[[ "$(json "$WORKDIR/stand-susp.json" '["to"]')" == "restricted" ]] \
  || { cat "$WORKDIR/stand-susp.json"; fail "the response does not say what the account holds now"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from users where id = '$stand_user_id';")" == "restricted" ]] \
  || fail "the account is not restricted"
ok "an account is restricted, and the response carries both ends — a console rendering only the new standing cannot tell a tightening from a loosening"

[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log
    where target_id = '$stand_user_id' and target_type = 'user'
      and action = 'user.standing_changed' and actor_id = '$unpub_id'
      and reason = '$stand_reason'
      and metadata->>'from' = 'active' and metadata->>'to' = 'restricted';")" == "1" ]] \
  || fail "the change wrote no audit entry carrying the reason and both ends"
ok "and the audit entry names the account, the administrator, the reason and both ends of the change"


# --- a no-op, and putting the account back ------------------------------------------------------------

status="$(standing "$unpub_token" "verify-adm161-noop-$$" "$stand_user_id" "$stand_body" noop)"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/stand-noop.json"; fail "setting the standing it already holds returned $status, want 409"; }
[[ "$(cat "$WORKDIR/stand-noop.json")" == *'admin_user_standing_unchanged'* ]] \
  || { cat "$WORKDIR/stand-noop.json"; fail "the refusal does not carry the code a console branches on"; }
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log where target_id = '$stand_user_id';")" == "1" ]] \
  || fail "a no-op wrote a second entry, in a table whose value is that everything in it happened"
ok "setting the standing an account already holds is refused rather than recorded — an entry saying restricted to restricted is noise in the one table that must be all signal"

status="$(standing "$unpub_token" "verify-adm161-reinstate-$$" "$stand_user_id" \
  '{"standing":"active","reason":"No-shows explained and evidenced; access restored after review."}' back)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/stand-back.json"; fail "reinstating returned $status, want 200"; }
[[ "$(json "$WORKDIR/stand-back.json" '["from"]')" == "restricted" ]] \
  || { cat "$WORKDIR/stand-back.json"; fail "the reinstatement does not say what the account held"; }

status="$(post_json "verify-adm161-login3-$$" /v1/auth/login \
  "{\"email\":\"$stand_email\",\"password\":\"$stand_password\",\"device_label\":\"Verify Standing\"}" "$WORKDIR/stand-login-back.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/stand-login-back.json"; fail "a reinstated account cannot sign in ($status)"; }
ok "and reinstatement is the same endpoint, the same permission and the same audit action — a trail recording a restriction but not its reversal makes a person look permanently suspect"

[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log
    where target_id = '$stand_user_id' and action = 'user.standing_changed';")" == "2" ]] \
  || fail "the reinstatement is not on the trail beside the suspension"

# `restricted` is the limited half, and it is not `suspended`: a restricted account may still sign
# in and read, which is the point — somebody who cannot sign in cannot read why they are restricted.
status="$(standing "$unpub_token" "verify-adm161-restrict-$$" "$stand_user_id" \
  '{"standing":"restricted","reason":"Insurance certificate expired; bidding paused until renewed."}' restr)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/stand-restr.json"; fail "restricting returned $status, want 200"; }

status="$(post_json "verify-adm161-login4-$$" /v1/auth/login \
  "{\"email\":\"$stand_email\",\"password\":\"$stand_password\",\"device_label\":\"Verify Standing\"}" "$WORKDIR/stand-login-restr.json")"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/stand-login-restr.json"; fail "a restricted account cannot sign in ($status); restricted is limited access, not disabled access"; }
ok "a restricted account still signs in and a suspended one does not — 'limited or disabled' is two standings, and an account that cannot sign in cannot read why it is restricted"

status="$(standing "$unpub_token" "verify-adm161-missing-$$" \
  "$(uuidgen 2>/dev/null || python3 -c 'import uuid;print(uuid.uuid4())')" "$stand_body" missing)"
[[ "$status" == "404" ]] \
  || { cat "$WORKDIR/stand-missing.json"; fail "an account that does not exist returned $status, want 404"; }
ok "an account that does not exist is a plain 404 — the caller is an administrator, and unlike dispute intake nothing is being kept from them"

admin_clear_limits

# ==========================================================================================
# SHIP-162 — internal support notes.
#
# # What only this section can show, and it is the whole second half of the ticket
#
# The *Done when* is two claims. "Notes attach to a user or a job" is established by the Go suite in
# `internal/admin`, which drives both handlers against a real database.
#
# **"And are never user-visible" cannot be established there at all.** Every test in that package
# drives an *administrative* handler, so a note not appearing in an administrative response proves
# nothing — the interesting claim is about a **customer's** endpoint, in a different domain, reached
# with a different credential. So this section writes a note on a job and then reads that job back
# **as its customer**, over HTTP, and fails if the note's text appears anywhere in the response.
#
# That check survives somebody widening the customer's job shape later, which is the failure mode
# the claim actually has: nobody sets out to publish a support note, they add a field.
#
# # Everything is fenced on rows this run created
#
# The note body carries this run's process id, so the absence check below is an assertion about a
# string that exists nowhere else in the database.

ticket "SHIP-162  support notes attach to a user or a job and are never user-visible"

admin_clear_limits

# note_add <token> <key> <body-json> <name> — one attempt, answering with its status.
note_add() {
  curl -s -X POST -o "$WORKDIR/note-$4.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$3" \
    "http://localhost:$VERIFY_PORT/v1/admin/notes"
}

# note_read <token> <subject-type> <subject-id> <name> — one read, answering with its status.
note_read() {
  curl -s -o "$WORKDIR/note-$4.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" \
    "http://localhost:$VERIFY_PORT/v1/admin/notes?subject_type=$2&subject_id=$3"
}

# The moderator and the support administrator this file already created for SHIP-160 — `notes.write`
# is a moderator's and reading is gated on the *subject's* read permission, which every role holds.
note_secret="conf1dential-$$-do-not-show-the-customer"
note_job="$(dispute_draft ship162job)"
note_body="{\"subject_type\":\"job\",\"subject_id\":\"$note_job\",\"body\":\"$note_secret — escalated to the insurer, do not discuss with the customer.\"}"

status="$(note_add "$unpub_token" "verify-adm162-add-$$" "$note_body" add)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/note-add.json"; fail "adding a note returned $status, want 201"; }
[[ "$(json "$WORKDIR/note-add.json" '["subject_type"]')" == "job" ]] \
  || { cat "$WORKDIR/note-add.json"; fail "the note is not attached to a job"; }
[[ "$(json "$WORKDIR/note-add.json" '["subject_id"]')" == "$note_job" ]] \
  || { cat "$WORKDIR/note-add.json"; fail "the note is attached to the wrong job"; }
[[ "$(json "$WORKDIR/note-add.json" '["author_id"]')" == "$unpub_id" ]] \
  || { cat "$WORKDIR/note-add.json"; fail "the note does not name the administrator who wrote it"; }
ok "a support note attaches to a job and names the administrator who wrote it"

# A note on a user, through the same endpoint. Both kinds, because a polymorphic subject with one
# kind exercised is a column that happens to hold one value.
note_user_body="{\"subject_type\":\"user\",\"subject_id\":\"$stand_user_id\",\"body\":\"$note_secret — two no-shows; watch the next booking.\"}"
status="$(note_add "$unpub_token" "verify-adm162-adduser-$$" "$note_user_body" adduser)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/note-adduser.json"; fail "adding a note on a user returned $status, want 201"; }
ok "and to a user, through the same endpoint — one place the never-user-visible rule has to hold rather than two"

# --- the audit entry names the subject, not the note ------------------------------------------------

[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log
    where target_id = '$note_job' and target_type = 'job'
      and action = 'note.added' and actor_id = '$unpub_id'
      and metadata ? 'note_id';")" == "1" ]] \
  || fail "adding a note wrote no audit entry naming the job it was about"

# And the body is deliberately NOT in the entry: audit_log is append-only and admin_notes is not, so
# a copy there would be an uncorrectable copy of a correctable record.
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log where target_id = '$note_job' and metadata::text like '%$note_secret%';")" == "0" ]] \
  || fail "the note's body was copied into the audit trail, which nothing can rewrite"
ok "the entry names the subject rather than the note, carries the note's id and does not carry its body — so 'everything that happened to this job' includes the notes without copying them into a table nothing can correct"

# --- reading them back, and the permission split ------------------------------------------------------

status="$(note_read "$unpub_support_token" job "$note_job" readsup)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/note-readsup.json"; fail "a support administrator could not read notes ($status)"; }
[[ "$(cat "$WORKDIR/note-readsup.json")" == *"$note_secret"* ]] \
  || { cat "$WORKDIR/note-readsup.json"; fail "the note is not in what support reads back"; }

status="$(note_add "$unpub_support_token" "verify-adm162-supwrite-$$" "$note_body" supwrite)"
[[ "$status" == "403" ]] \
  || { cat "$WORKDIR/note-supwrite.json"; fail "a support administrator added a note and got $status, want 403"; }
ok "a support administrator reads the history and cannot add to it — reading is gated on the subject's own permission, which every role holds, and writing on notes.write, which support does not"

# A subject with no notes is an empty list rather than a 404: the subject is deliberately not looked
# up, so "no notes" and "no such subject" are the same answer here.
status="$(note_read "$unpub_token" user "$(uuidgen 2>/dev/null || python3 -c 'import uuid;print(uuid.uuid4())')" readempty)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/note-readempty.json"; fail "reading notes on a subject with none returned $status, want 200"; }
[[ "$(cat "$WORKDIR/note-readempty.json")" == *'"data":[]'* ]] \
  || { cat "$WORKDIR/note-readempty.json"; fail "an empty history is not an empty array; null crashes a console that iterates"; }
ok "a subject with no notes answers with an empty array rather than a 404 — the subject is not looked up, because a note outlives it"

# --- the claim the Go suite structurally cannot make --------------------------------------------------
#
# Read the job back **as its customer**, with the mobile credential, and fail if the note is anywhere
# in the response. This is an assertion about internal/jobs' endpoint rather than about this one,
# which is why it survives somebody widening that shape later.

status="$(curl -s -o "$WORKDIR/note-customer-job.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $dispute_customer_token" \
  "http://localhost:$VERIFY_PORT/v1/jobs/$note_job")"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/note-customer-job.json"; fail "the customer could not read their own job ($status), so the check below would pass vacuously"; }
[[ "$(cat "$WORKDIR/note-customer-job.json")" != *"$note_secret"* ]] \
  || { cat "$WORKDIR/note-customer-job.json"; fail "a support note appeared in the customer's own view of their job"; }
ok "the job's customer reads their own job and the note is not in it — the claim no test in internal/admin can make, because it is about another domain's endpoint"

# The same job through the customer's list, which is a different shape built by a different query.
status="$(curl -s -o "$WORKDIR/note-customer-list.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $dispute_customer_token" \
  "http://localhost:$VERIFY_PORT/v1/jobs?limit=50")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/note-customer-list.json"; fail "the customer could not list their jobs ($status)"; }
[[ "$(cat "$WORKDIR/note-customer-list.json")" != *"$note_secret"* ]] \
  || { cat "$WORKDIR/note-customer-list.json"; fail "a support note appeared in the customer's job list"; }
ok "and it is not in their job list either — two shapes, two queries, and the note is in neither"

# --- a note that records nothing, and one about something the platform does not have -------------------

status="$(note_add "$unpub_token" "verify-adm162-empty-$$" \
  "{\"subject_type\":\"job\",\"subject_id\":\"$note_job\",\"body\":\"   \"}" empty)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/note-empty.json"; fail "an empty note returned $status, want 422"; }
[[ "$(cat "$WORKDIR/note-empty.json")" == *'"body"'* ]] \
  || { cat "$WORKDIR/note-empty.json"; fail "the refusal does not name the field"; }

status="$(note_add "$unpub_token" "verify-adm162-badsubj-$$" \
  "{\"subject_type\":\"administrator\",\"subject_id\":\"$unpub_id\",\"body\":\"About a colleague.\"}" badsubj)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/note-badsubj.json"; fail "a note about an administrator returned $status, want 422"; }
[[ "$(cat "$WORKDIR/note-badsubj.json")" == *'"subject_type"'* ]] \
  || { cat "$WORKDIR/note-badsubj.json"; fail "the refusal does not name the field"; }
ok "a note with nothing in it and one about a kind of thing this table does not hold are both refused and name their field"

status="$(note_read "" job "$note_job" nocred)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/note-nocred.json"; fail "notes are readable without a credential ($status)"; }
[[ "$(cat "$WORKDIR/note-nocred.json")" != *"$note_secret"* ]] \
  || fail "a refused read returned the note anyway"
ok "and the history is behind the administrator credential — without one it would be a public file on every customer the platform has had a problem with"

admin_clear_limits

# ==========================================================================================
# SHIP-147b — an administrator's idempotency key is scoped to that administrator.
#
# # Why this needs the harness rather than a Go test
#
# `cmd/api`'s TestTwoAdministratorSessionsDoNotShareAnIdempotencyScope drives the same two requests
# through the same router, and it is the stronger of the two in one way — it reads `admin_notes`
# back and counts the rows. What it cannot do is prove it against **Redis**. It uses the in-memory
# idempotency store, because a `-race` test that reached the shared cache would be a test the whole
# suite could not run in parallel; the deployed service uses `idempotency.RedisStore`, and the key
# that store writes is `idem:v1:<scope>:<key>` in a real keyspace.
#
# So this section asserts on the thing no Go test in this repository looks at: **the keys actually
# in Redis**. If the scope had reverted to its user-only form there would be exactly one key,
# `idem:v1:anonymous:<key>`, holding one administrator's note where the other's request would find
# it.
#
# # Two moderators, one key, one body
#
# `replayOrRefuse` fingerprints method, path and body, so with those identical the scope is the only
# thing that can separate the two requests. Both are moderators because `notes.write` is a
# moderator's permission, and a note's subject is deliberately not a foreign key (SHIP-162), so the
# job it names need not be one either.

ticket "SHIP-147b  two administrators sending one idempotency key get one execution each"

admin_clear_limits

# The second moderator. `$unpub_token`/`$unpub_id` above is the first.
scope_email="verify-admin-scope-$$@example.com"
scope_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$scope_email', 'Verify Moderator Two', '$admin_fixture_hash', 'moderator')
   returning id;")"
[[ -n "$scope_id" ]] || fail "the second moderator could not be created"
[[ "$scope_id" != "$unpub_id" ]] || fail "the two administrators are the same account, so this section proves nothing"

status="$(admin_signin "verify-adm147b-signin-$$" "$scope_email" "$admin_password" scope-in)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-scope-in.json"; fail "the second moderator could not sign in ($status)"; }
scope_token="$(json "$WORKDIR/admin-scope-in.json" '["token"]')"
[[ -n "$scope_token" ]] || fail "the second moderator's sign-in returned no token"
[[ "$scope_token" != "$unpub_token" ]] || fail "both administrators hold the same credential"

# scope_note <token> <name> — the same POST, keeping the response headers.
#
# One key and one body, both fixed, because they are what makes the two requests indistinguishable
# to everything except the scope.
scope_key="verify-adm147b-shared-$$"
scope_job="$(dispute_draft ship147bjob)"
scope_body="{\"subject_type\":\"job\",\"subject_id\":\"$scope_job\",\"body\":\"Rang the depot about this one.\"}"

scope_note() {
  curl -s -X POST -o "$WORKDIR/scope-$2.json" -D "$WORKDIR/scope-$2.headers" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $scope_key" \
    -H 'Content-Type: application/json' -d "$scope_body" \
    "http://localhost:$VERIFY_PORT/v1/admin/notes"
}

scope_replayed() { grep -qi '^idempotency-replayed: true' "$WORKDIR/scope-$1.headers"; }

status="$(scope_note "$unpub_token" first)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/scope-first.json"; fail "the first administrator's note returned $status, want 201"; }
! scope_replayed first || fail "the first request was already a replay; the key is not fresh"
first_note="$(json "$WORKDIR/scope-first.json" '["id"]')"
[[ "$(json "$WORKDIR/scope-first.json" '["author_id"]')" == "$unpub_id" ]] \
  || { cat "$WORKDIR/scope-first.json"; fail "the note does not name the administrator who wrote it"; }

status="$(scope_note "$scope_token" second)"
! scope_replayed second \
  || { cat "$WORKDIR/scope-second.json"; fail "the second administrator was handed the first one's stored response — two administrator sessions share an idempotency namespace (SHIP-147b)"; }
[[ "$status" == "201" ]] || { cat "$WORKDIR/scope-second.json"; fail "the second administrator's note returned $status, want 201"; }

second_note="$(json "$WORKDIR/scope-second.json" '["id"]')"
[[ "$second_note" != "$first_note" ]] || fail "both administrators were given note $first_note; only one execution happened"
[[ "$(json "$WORKDIR/scope-second.json" '["author_id"]')" == "$scope_id" ]] \
  || { cat "$WORKDIR/scope-second.json"; fail "the second response names the wrong author — the second administrator was served the first one's note"; }
ok "two administrators sending one key, one method, one path and one body each get their own execution and their own response"

# The rows, not the responses. A replay answers without the handler running, so this is what
# separates two executions from one execution answered twice.
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(distinct author_id) from admin_notes where subject_id = '$scope_job';")" == "2" ]] \
  || fail "the notes on this job do not have two distinct authors, so only one administrator's request ran"
ok "and both notes are in admin_notes, written by two different administrators"

# --- the keys in Redis, which is the half no Go test looks at ------------------------------------
#
# Two entries, one per credential, and **no `idem:v1:anonymous:` entry for this key** — which is
# exactly the state the user-only scope would have produced instead.
#
# The scope is a digest of the credential rather than the administrator it names. That is not the
# obvious choice and it was not the first one: a scope that has to *resolve* a credential is not
# stable across that credential's own revocation, and `DELETE /v1/admin/sessions/current` revokes
# the credential it is called with — so the retried sign-out asserted a few hundred lines above is
# the check that found it. httpx.SubjectScope carries the argument in full.

scope_keys="$(redis-cli -u "$REDIS_URL" --scan --pattern "idem:v1:*:$scope_key" | sort)"
[[ "$(printf '%s\n' "$scope_keys" | grep -c .)" == "2" ]] \
  || fail "this key produced these idempotency entries, want two:
$scope_keys"
[[ "$(printf '%s\n' "$scope_keys" | grep -c '^idem:v1:credential:')" == "2" ]] \
  || fail "the two entries are not both namespaced to a credential:
$scope_keys"
! printf '%s\n' "$scope_keys" | grep -q "^idem:v1:anonymous:" \
  || fail "an administrator's key landed in the shared anonymous namespace:
$scope_keys"
ok "the stored keys are idem:v1:credential:<digest>:<key>, one per administrator, and neither is in the anonymous namespace"

# Neither key carries a credential, and neither carries the digest the database stores.
#
# The second half is why the digest is salted. `admin_sessions.token_hash` is `sha256(token)`, so an
# unsalted scope would render the database's stored verifier into a cache key — readable by
# anything that can run SCAN, and by every log line that names one. This asserts it against the
# actual column rather than against a restatement of the rule.
for scope_secret in "$unpub_token" "$scope_token"; do
  ! printf '%s\n' "$scope_keys" | grep -qF "$scope_secret" \
    || fail "an idempotency key contains a live administrator credential:
$scope_keys"
done
for scope_stored in $("$PSQL" "$DATABASE_URL" -qtAc "select token_hash from admin_sessions;"); do
  ! printf '%s\n' "$scope_keys" | grep -qF "$scope_stored" \
    || fail "an idempotency key contains admin_sessions.token_hash ($scope_stored), so the cache is publishing the digest the database compares against:
$scope_keys"
done
ok "and neither key carries the credential itself, nor the digest admin_sessions stores against it"

# --- and idempotency still works within one administrator ----------------------------------------
#
# Without this the section above would be satisfied by a change that disabled the mechanism rather
# than scoping it — two executions is the right answer for two administrators and the wrong one for
# one administrator retrying.

status="$(scope_note "$unpub_token" replay)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/scope-replay.json"; fail "the first administrator's retry returned $status, want the stored 201"; }
scope_replayed replay || fail "the same administrator repeating the same key did not get a replay; idempotency has been scoped into uselessness rather than scoped correctly"
[[ "$(json "$WORKDIR/scope-replay.json" '["id"]')" == "$first_note" ]] \
  || fail "the replay returned a different note, so a second row was written"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from admin_notes where subject_id = '$scope_job';")" == "2" ]] \
  || fail "the retry wrote a third note"
ok "one administrator retrying the same key is still answered from the store, and writes nothing"

# --- an invented credential reaches nobody, and `anonymous` is still there ------------------------
#
# **This check asserts something different from the one it replaces, and the change is deliberate.**
# While the scope was resolved from the credential an unrecognised one fell back to `anonymous`, and
# this asserted that. A credential digest does not fall back: anybody may invent a bearer token and
# be given a namespace of their own. That is not a weakness — a scope nobody else can compute is a
# scope nobody else is in, and an invented token still cannot open an administrative route — so what
# is asserted here is the property that carries the weight instead: an invented credential lands
# nowhere near a real administrator's entries, and a caller who presents *nothing* is still
# anonymous, which every public route depends on.

invented_key="verify-adm147b-invented-$$"
status="$(curl -s -X POST -o "$WORKDIR/scope-invented.json" -w '%{http_code}' \
  -H "$auth_header: Bearer not-an-administrator-session-$$" -H "Idempotency-Key: $invented_key" \
  -H 'Content-Type: application/json' -d "$scope_body" \
  "http://localhost:$VERIFY_PORT/v1/admin/notes")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/scope-invented.json"; fail "an invented credential returned $status, want 401"; }

invented_keys="$(redis-cli -u "$REDIS_URL" --scan --pattern "idem:v1:*:$invented_key" | sort)"
[[ "$(printf '%s\n' "$invented_keys" | grep -c .)" == "1" ]] \
  || fail "an invented credential produced these entries, want one:
$invented_keys"
printf '%s\n' "$invented_keys" | grep -q "^idem:v1:credential:" \
  || fail "an invented credential did not get a namespace of its own:
$invented_keys"
invented_scope="$(printf '%s\n' "$invented_keys" | sed 's/:[^:]*$//')"
! printf '%s\n' "$scope_keys" | grep -qF "$invented_scope:" \
  || fail "an invented credential landed in a namespace a real administrator is using:
$invented_keys
$scope_keys"
ok "an invented credential is refused, and its key lands in a namespace no administrator is in"

nocred_key="verify-adm147b-nocred-$$"
status="$(curl -s -X POST -o "$WORKDIR/scope-nocred.json" -w '%{http_code}' \
  -H "Idempotency-Key: $nocred_key" -H 'Content-Type: application/json' -d "$scope_body" \
  "http://localhost:$VERIFY_PORT/v1/admin/notes")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/scope-nocred.json"; fail "a request with no credential returned $status, want 401"; }
[[ -n "$(redis-cli -u "$REDIS_URL" --scan --pattern "idem:v1:anonymous:$nocred_key")" ]] \
  || fail "a caller who presented nothing did not land in the anonymous namespace, which every public route depends on"
ok "and a caller who presents nothing is still anonymous, so the shared scope every public route uses is intact"

admin_clear_limits

# ==========================================================================================
# SHIP-153 and SHIP-154 — the verification review queue, and the decision taken from it.
#
# # What this section demonstrates that no Go test can
#
# `internal/admin` may not import `internal/profiles`, so the two meet only in `cmd/api` — in
# `providerVerifications`, an adapter no test in package main can drive against a database. The Go
# suite in `internal/admin` exercises a *copy* of that adapter, which proves the domain and proves
# nothing about the one the running service registers. This section drives the built binary, so the
# adapter under test is the one that ships.
#
# It is also where the two credential systems are shown apart: the decision is reachable on an
# administrator session and on nothing else, which is the claim `cmd/api/routes_profiles.go` made in
# a comment before either endpoint existed.
#
# # The queue is a shared, never-reset table, so every assertion is fenced by id or by clock
#
# `make verify` runs against a database that is not reset between runs, and **every provider ever
# registered by any section of any run is on the Pending queue** — SHIP-81a gives one a record at
# registration. So a count would be meaningless and "the first page" would be whatever history left
# there. The three providers below are backdated to 1990, which puts them at the head of an
# oldest-first queue no matter how much precedes them, and every other check names an identifier.
#
# **The mobile prefix is 04197 and 04198**, which are this section's; 04190…04196 are taken by the
# sections above (see the block at the head of this file).

ticket "SHIP-153  pending provider verifications are listed oldest first"

admin_clear_limits

# A moderator and a support administrator of this section's own, rather than the ones the SHIP-160
# section signed in: `verifications.read` and `verifications.decide` split across those two roles is
# exactly what this section is about, and a token borrowed from another section is one another
# section can revoke.
verif_mod_email="verify-admin-verif-mod-$$@example.com"
verif_mod_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$verif_mod_email', 'Verify Reviewer', '$admin_fixture_hash', 'moderator')
   returning id;")"
[[ -n "$verif_mod_id" ]] || fail "the reviewing moderator could not be created"

status="$(admin_signin "verify-adm153-modin-$$" "$verif_mod_email" "$admin_password" verif-mod)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-verif-mod.json"; fail "the reviewing moderator could not sign in ($status)"; }
verif_mod_token="$(json "$WORKDIR/admin-verif-mod.json" '["token"]')"

verif_sup_email="verify-admin-verif-sup-$$@example.com"
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$verif_sup_email', 'Verify Looker', '$admin_fixture_hash', 'support');" >/dev/null \
  || fail "the support administrator could not be created"
status="$(admin_signin "verify-adm153-supin-$$" "$verif_sup_email" "$admin_password" verif-sup)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-verif-sup.json"; fail "the support administrator could not sign in ($status)"; }
verif_sup_token="$(json "$WORKDIR/admin-verif-sup.json" '["token"]')"

# verif_provider <suffix> <name> — a registered provider, answering with its id.
#
# Registered through the API rather than inserted, because `000200`'s trigger firing on a real
# registration is half of what makes the queue non-empty at all.
verif_provider() {
  local out="$WORKDIR/verif-provider-$1.json"
  local code
  code="$(post_json "verify-adm153-reg-$1-$$" /v1/auth/register \
    "{\"name\":\"$2\",\"email\":\"verify-verif-$1-$$@example.com\",\"phone\":\"0419$3$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
    "$out")"
  [[ "$code" == "201" ]] || { cat "$out"; fail "could not register the provider $1: $code"; }
  json "$out" '["id"]'
}

verif_first="$(verif_provider first "Ngaire Tuiavii" 7)"
verif_second="$(verif_provider second "Boadicea Kellsworth" 8)"

# A customer, which must never appear on a queue of providers — most accounts on this platform are
# customers, and a queue holding them is a queue of people nobody will ever review.
status="$(post_json "verify-adm153-cust-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"verify-verif-cust-$$@example.com\",\"phone\":\"04197$$1\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/verif-customer.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/verif-customer.json"; fail "could not register the verification customer: $status"; }
verif_customer_id="$(json "$WORKDIR/verif-customer.json" '["id"]')"

# The fence. Backdated to 1990 in a known order, so these two are the head of an oldest-first queue
# whatever else the database is carrying — and deliberately backdated in the *reverse* of the order
# they were registered, so a queue reading the insertion sequence rather than the submission clock
# fails here rather than passing by accident.
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 >/dev/null <<SQL
update provider_verifications set created_at = timestamptz '1990-01-02 00:00:00Z' where provider_id = '$verif_second';
update provider_verifications set created_at = timestamptz '1990-01-01 00:00:00Z' where provider_id = '$verif_first';
SQL

# verif_queue <state> <token> <name> [extra] — one read of the queue, answering with its status.
verif_queue() {
  admin_get "/v1/admin/verifications?state=$1${4:+&$4}" "$2" "$3"
}

# verif_at <name> <index> — the provider id at that position on the page.
verif_at() {
  python3 - "$WORKDIR/admin-$1.json" "$2" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
rows = page.get("data") or []
index = int(sys.argv[2])
print(rows[index]["provider_id"] if index < len(rows) else "")
PY
}

# verif_carries <name> <provider-id> — 1 when the provider is on the page, 0 when not.
verif_carries() {
  python3 - "$WORKDIR/admin-$1.json" "$2" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
print(sum(1 for e in (page.get("data") or []) if e["provider_id"] == sys.argv[2]))
PY
}

status="$(curl -s -o "$WORKDIR/verif-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/admin/verifications?state=Pending")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/verif-anon.json"; fail "the queue answered $status without a credential, want 401"; }
ok "the queue cannot be read without an administrator session"

status="$(verif_queue Pending "$verif_sup_token" verif-sup-queue "limit=2")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-verif-sup-queue.json"; fail "a support administrator could not read the queue: $status"; }
[[ "$(verif_at verif-sup-queue 0)" == "$verif_first" ]] \
  || { cat "$WORKDIR/admin-verif-sup-queue.json"; fail "the first entry is not the longest-waiting provider"; }
[[ "$(verif_at verif-sup-queue 1)" == "$verif_second" ]] \
  || { cat "$WORKDIR/admin-verif-sup-queue.json"; fail "the second entry is not the next-longest-waiting provider"; }
ok "pending providers are listed oldest first, and a support administrator may read them — looking is what the least-privileged role exists to be able to do"

[[ "$(verif_carries verif-sup-queue "$verif_customer_id")" == "0" ]] \
  || fail "a customer is on the provider verification queue"
status="$(verif_queue Pending "$verif_mod_token" verif-wide "limit=100")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-verif-wide.json"; fail "the queue returned $status"; }
[[ "$(verif_carries verif-wide "$verif_customer_id")" == "0" ]] \
  || fail "a customer appears on a wider page of the provider verification queue"
ok "and a customer is on neither page — the record is a provider's, and most accounts here are not"

# What the entry carries, and the closed key set that is where a budget would arrive. Checked as a
# key set rather than by searching for the word: SHIP-83 established that a spelling-based guard
# misses a field named anything at all.
python3 - "$WORKDIR/admin-verif-sup-queue.json" "$verif_first" <<'PY' || fail "the queue entry shape is wrong"
import json, sys
page = json.load(open(sys.argv[1]))
entry = next(e for e in page["data"] if e["provider_id"] == sys.argv[2])
allowed = {"provider_id", "name", "email", "phone", "state", "submitted_at"}
extra = set(entry) - allowed
missing = allowed - set(entry)
if extra:
    print("the entry carries", sorted(extra))
    sys.exit(1)
if missing:
    print("the entry is missing", sorted(missing))
    sys.exit(1)
if entry["name"] != "Ngaire Tuiavii":
    print("the entry does not name the provider:", entry["name"])
    sys.exit(1)
if entry["state"] != "Pending":
    print("a Pending queue entry reads", entry["state"])
    sys.exit(1)
PY
ok "an entry names who the provider is and carries nothing commercial — six keys, and no shape to put a budget in"

status="$(verif_queue Approved "$verif_mod_token" verif-badstate)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/admin-verif-badstate.json"; fail "an outcome Docs 04 does not have returned $status, want 422"; }
[[ "$(cat "$WORKDIR/admin-verif-badstate.json")" == *'"state"'* ]] \
  || { cat "$WORKDIR/admin-verif-badstate.json"; fail "the refusal does not name the field"; }
status="$(admin_get "/v1/admin/verifications" "$verif_mod_token" verif-nostate)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/admin-verif-nostate.json"; fail "a queue with no state returned $status, want 422"; }
ok "a state the document does not have, and no state at all, are both refused — an ignored filter answers an empty page, and an empty review queue is what 'nobody is waiting' looks like"

admin_clear_limits

# ==========================================================================================
ticket "SHIP-154  a reviewer records an outcome with a reason, and the audit entry commits with it"

admin_clear_limits

# verif_decide <token> <key> <provider> <body> <name> — one decision, answering with its status.
verif_decide() {
  curl -s -X POST -o "$WORKDIR/verif-dec-$5.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$4" \
    "http://localhost:$VERIFY_PORT/v1/admin/verifications/$3/decision"
}

verif_state() {
  "$PSQL" "$DATABASE_URL" -tAc "select state from provider_verifications where provider_id = '$1';"
}

verif_reason="Licence, registration, insurance and ABN evidence all current and matching the account."

# --- the permission, before anything succeeds -----------------------------------------------------

status="$(verif_decide "$verif_sup_token" "verify-adm154-sup-$$" "$verif_first" \
  "{\"state\":\"Verified\",\"reason\":\"$verif_reason\"}" sup)"
[[ "$status" == "403" ]] || { cat "$WORKDIR/verif-dec-sup.json"; fail "a support administrator decided a verification and got $status, want 403"; }
[[ "$(verif_state "$verif_first")" == "Pending" ]] || fail "a refused decision moved the provider anyway"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from provider_verification_decisions where provider_id = '$verif_first';")" == "0" ]] \
  || fail "a refused decision wrote into the evidence trail"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log where target_id = '$verif_first';")" == "0" ]] \
  || fail "a refused decision wrote an audit entry, in a table nothing can correct"
ok "a support administrator may read the queue and may not decide what is in it — Docs 04 §9's least privilege as two permissions on two endpoints"

# --- a reason that records nothing, and an outcome the document does not have -----------------------

status="$(verif_decide "$verif_mod_token" "verify-adm154-noreason-$$" "$verif_first" \
  '{"state":"Verified","reason":"ok"}' noreason)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/verif-dec-noreason.json"; fail "a reason too short to record anything returned $status, want 422"; }
[[ "$(cat "$WORKDIR/verif-dec-noreason.json")" == *'"reason"'* ]] \
  || { cat "$WORKDIR/verif-dec-noreason.json"; fail "the refusal does not name the field"; }

status="$(verif_decide "$verif_mod_token" "verify-adm154-badstate-$$" "$verif_first" \
  "{\"state\":\"Approved\",\"reason\":\"$verif_reason\"}" badstate)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/verif-dec-badstate.json"; fail "an outcome Docs 04 does not have returned $status, want 422"; }
for outcome in Pending Verified Restricted Rejected Suspended; do
  grep -q "$outcome" "$WORKDIR/verif-dec-badstate.json" \
    || { cat "$WORKDIR/verif-dec-badstate.json"; fail "the refusal does not offer $outcome as a choice"; }
done
ok "a reason that records nothing is refused, and an unknown outcome is refused with Docs 04 §4's five named — the list comes from the domain that owns it, not from a copy in the console"

# --- the decision itself, and the two tables it writes ----------------------------------------------

status="$(verif_decide "$verif_mod_token" "verify-adm154-verify-$$" "$verif_first" \
  "{\"state\":\"Verified\",\"reason\":\"$verif_reason\"}" verify)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/verif-dec-verify.json"; fail "the decision returned $status, want 200"; }
[[ "$(json "$WORKDIR/verif-dec-verify.json" '["from"]')" == "Pending" ]] \
  || { cat "$WORKDIR/verif-dec-verify.json"; fail "the response does not say what the provider held before"; }
[[ "$(json "$WORKDIR/verif-dec-verify.json" '["to"]')" == "Verified" ]] \
  || { cat "$WORKDIR/verif-dec-verify.json"; fail "the response does not say what the provider holds now"; }
[[ "$(verif_state "$verif_first")" == "Verified" ]] || fail "the provider is not Verified"
ok "a moderator records an outcome and the response carries both ends — one endpoint serves five outcomes, so a console shown only the new one cannot tell a tightening from a loosening"

# The provider's evidence trail (Docs 04 §1), which is the table the review has to be reconstructible
# from years later.
trail="$("$PSQL" "$DATABASE_URL" -tAc \
  "select from_state || ' ' || to_state || ' ' || actor_type || ' ' || (actor_id = '$verif_mod_id')::text
     from provider_verification_decisions where provider_id = '$verif_first';")"
[[ "$trail" == "Pending Verified admin true" ]] \
  || fail "the recorded decision is '$trail', want 'Pending Verified admin true'"

# The administrator's accountability record (Docs 04 §9), which is a different table for a different
# reader — and the same reason written into both, deliberately.
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log
    where target_id = '$verif_first' and target_type = 'user'
      and action = 'verification.decided' and actor_id = '$verif_mod_id'
      and reason = '$verif_reason'
      and metadata->>'from' = 'Pending' and metadata->>'to' = 'Verified';")" == "1" ]] \
  || fail "the decision wrote no audit entry carrying the administrator, the reason and both ends"
ok "the decision is in two tables — the provider's evidence trail and the administrator's audit entry — and the reason is in both, because a reader of either should not have to find the other"

# --- the queue moves, which is what makes it a queue -------------------------------------------------

status="$(verif_queue Pending "$verif_mod_token" verif-after "limit=2")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-verif-after.json"; fail "the queue returned $status"; }
[[ "$(verif_carries verif-after "$verif_first")" == "0" ]] \
  || { cat "$WORKDIR/admin-verif-after.json"; fail "a decided provider is still on the Pending queue"; }
[[ "$(verif_at verif-after 0)" == "$verif_second" ]] \
  || { cat "$WORKDIR/admin-verif-after.json"; fail "the next-longest-waiting provider did not move to the head of the queue"; }

status="$(verif_queue Verified "$verif_mod_token" verif-verified "limit=100")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-verif-verified.json"; fail "the Verified queue returned $status"; }
[[ "$(verif_carries verif-verified "$verif_first")" == "1" ]] \
  || fail "a verified provider is on no queue at all — Docs 04 §5's first queue is new *or changed* submissions"
ok "the decided provider leaves the Pending queue and is findable on the one for the outcome they moved to"

# --- what the provider is told, which is the round trip the whole ticket exists for -------------------

verif_first_token="$(mint_token "$verif_first")"
status="$(curl -s -o "$WORKDIR/verif-provider-read.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $verif_first_token" \
  "http://localhost:$VERIFY_PORT/v1/provider/verification")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/verif-provider-read.json"; fail "the provider could not read their own record: $status"; }
[[ "$(json "$WORKDIR/verif-provider-read.json" '["state"]')" == "Verified" ]] \
  || { cat "$WORKDIR/verif-provider-read.json"; fail "the provider does not read Verified"; }
grep -q "matching the account" "$WORKDIR/verif-provider-read.json" \
  || { cat "$WORKDIR/verif-provider-read.json"; fail "the reason did not reach the provider"; }
for disclosure in admin_id decided_by reviewer administrator "$verif_mod_id" "$verif_mod_email"; do
  grep -qi -- "$disclosure" "$WORKDIR/verif-provider-read.json" \
    && { cat "$WORKDIR/verif-provider-read.json"; fail "the provider's record discloses '$disclosure'"; }
done
ok "an administrator decides and the provider reads the outcome and the reason — and never who decided it, which is what keeps a moderation decision from becoming a personal one"

# --- a decision that changes nothing, and one on an account that has no record ------------------------

status="$(verif_decide "$verif_mod_token" "verify-adm154-noop-$$" "$verif_first" \
  "{\"state\":\"Verified\",\"reason\":\"Looked at it again and left it exactly as it was.\"}" noop)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/verif-dec-noop.json"; fail "re-deciding the outcome already held returned $status, want 409"; }
[[ "$(cat "$WORKDIR/verif-dec-noop.json")" == *'admin_verification_unchanged'* ]] \
  || { cat "$WORKDIR/verif-dec-noop.json"; fail "the refusal does not carry the code a console branches on"; }
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from provider_verification_decisions where provider_id = '$verif_first';")" == "1" ]] \
  || fail "a decision that changed nothing was recorded anyway"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log where target_id = '$verif_first';")" == "1" ]] \
  || fail "a refused decision wrote a second audit entry, in a table whose value is that everything in it happened"
ok "deciding the outcome a provider already holds is refused rather than recorded — on a queue two moderators are reading, that is the answer the second one gets"

status="$(verif_decide "$verif_mod_token" "verify-adm154-cust-$$" "$verif_customer_id" \
  "{\"state\":\"Verified\",\"reason\":\"$verif_reason\"}" cust)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/verif-dec-cust.json"; fail "deciding a customer's verification returned $status, want 404"; }
ok "an account with no verification record is a plain 404 — the caller is an administrator, and unlike dispute intake nothing is being kept from them"

# --- the second half of Docs 04 §4's vocabulary, and that any outcome may follow any other -----------
#
# Docs/04 §4 defines no transition table and the platform enforces none: a Suspended provider is
# reinstated, a Rejected one is Restricted after clarification. Four moves on one provider is the
# demonstration, and the trail keeps every one of them.

verif_step() {
  local code
  code="$(verif_decide "$verif_mod_token" "verify-adm154-$3-$$" "$verif_second" \
    "{\"state\":\"$1\",\"reason\":\"$2\"}" "$3")"
  [[ "$code" == "200" ]] || { cat "$WORKDIR/verif-dec-$3.json"; fail "deciding $1 returned $code"; }
  [[ "$(verif_state "$verif_second")" == "$1" ]] || fail "the provider is not $1"
}

verif_step Restricted "Insurance certificate expires this month; bidding paused until it is renewed." restrict
verif_step Rejected "The licence supplied is in a different name from the account holder's." reject
verif_step Suspended "Three unresolved safety reports from customers over one fortnight." suspend
verif_step Verified "Reports resolved and documents re-supplied; standing restored after review." reinstate

[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from provider_verification_decisions where provider_id = '$verif_second';")" == "4" ]] \
  || fail "the evidence trail does not hold all four decisions"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log where target_id = '$verif_second' and action = 'verification.decided';")" == "4" ]] \
  || fail "the audit trail does not hold all four decisions"
ok "all five of Docs 04 §4's outcomes are reachable and any may follow any other, including a reinstatement — and both trails keep every one of the four moves"

# --- there is exactly one route to this act, and it is on the administrator credential ---------------
#
# The claim `cmd/api/routes_profiles.go` made in a comment before either endpoint existed: a route to
# a decision on the *user* credential would be a second way to reach the same act on the wrong one,
# which is what Docs/04 §9's least-privilege requirement exists to prevent.

status="$(curl -s -X POST -o "$WORKDIR/verif-user-route.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $verif_first_token" -H "Idempotency-Key: verify-adm154-userroute-$$" \
  -H 'Content-Type: application/json' -d '{"state":"Verified","reason":"I decided this about myself."}' \
  "http://localhost:$VERIFY_PORT/v1/provider/verification")"
[[ "$status" == "405" ]] \
  || { cat "$WORKDIR/verif-user-route.json"; fail "POST /v1/provider/verification answered $status, want 405 — the provider's own record is read-only"; }

status="$(verif_decide "$verif_first_token" "verify-adm154-usertoken-$$" "$verif_second" \
  "{\"state\":\"Verified\",\"reason\":\"$verif_reason\"}" usertoken)"
[[ "$status" == "401" ]] \
  || { cat "$WORKDIR/verif-dec-usertoken.json"; fail "a user token reached the decision endpoint and got $status, want 401"; }
ok "a provider cannot decide their own standing and a user token cannot reach the administrator's endpoint — one act, one credential, one route"

admin_clear_limits

# ==========================================================================================
# SHIP-155 — the document viewer for private evidence.
#
# # What this section demonstrates that no Go test can
#
# `internal/admin`'s own suite drives the real `internal/profiles` reader over a **fake** object
# store, deliberately: what the domain has to get right is the record, and the schema enforces that.
# What is left over is exactly what this section is for.
#
#   1. **The signature is real.** A URL minted here is signed by `internal/platform/storage` against
#      MinIO with the deployment's own credential, and it is *spent* — the bytes come back. A
#      stubbed signer means no Go test in that package can fail when signing breaks.
#   2. **The adapter under test is the one that ships.** `providerEvidence` lives in package main,
#      where no test has a database to reach; the Go suite exercises a copy of it.
#   3. **The access entry is read out of `audit_log` by SQL**, on the same request that rendered the
#      images. Two clauses of one *Done when*, demonstrated together on one round trip, which is the
#      only place they meet.
#
# The provider is `verif_first`, already registered and decided by the two sections above, and every
# fixture below is this file's own — no variable from another section file is read.

ticket "SHIP-155  verification images render through short-lived signed URLs and are access-logged"

admin_clear_limits

# --- a document that actually exists in the bucket -------------------------------------------------
#
# Minted, uploaded and submitted through the platform's own three endpoints, because a row written
# with SQL would carry an object key naming nothing, a media type nobody measured and an entity tag
# nobody was given. Every one of those is the store's answer rather than anybody's choice, and the
# viewer below hands all three to a reviewer.

printf 'not a licence, but exactly fifty-two bytes of proof.' > "$WORKDIR/adm-evidence.bin"

# evid_submit <kind> <n> — one document into verif_first's file. Leaves the record in
# $WORKDIR/adm-evid-<kind>.json and the object key in $evid_last_key.
#
# It sets a global rather than printing, and is called directly rather than in a command
# substitution: `fail` exits, and inside `$( … )` that exits only the subshell — the harness would
# print the failure and carry on green.
evid_last_key=""
evid_submit() {
  local kind="$1" n="$2" url code
  code="$(curl -s -X POST -o "$WORKDIR/adm-evid-url-$kind.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $verif_first_token" -H "Idempotency-Key: verify-adm155-url-$kind-$n-$$" \
    -H 'Content-Type: application/json' -d '{"content_type":"image/jpeg","content_length":52}' \
    "http://localhost:$VERIFY_PORT/v1/provider/verification/documents/uploads")"
  [[ "$code" == "200" ]] \
    || { cat "$WORKDIR/adm-evid-url-$kind.json"; fail "minting an upload URL for $kind answered $code"; }

  evid_last_key="$(json "$WORKDIR/adm-evid-url-$kind.json" '["object_key"]')"
  url="$(json "$WORKDIR/adm-evid-url-$kind.json" '["upload_url"]')"

  code="$(curl -s -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: image/jpeg" \
    --data-binary "@$WORKDIR/adm-evidence.bin" "$url")"
  [[ "$code" == "200" ]] || fail "uploading the $kind answered $code"

  code="$(curl -s -X POST -o "$WORKDIR/adm-evid-$kind.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $verif_first_token" -H "Idempotency-Key: verify-adm155-rec-$kind-$n-$$" \
    -H 'Content-Type: application/json' \
    -d "{\"kind\":\"$kind\",\"object_key\":\"$evid_last_key\"}" \
    "http://localhost:$VERIFY_PORT/v1/provider/verification/documents")"
  [[ "$code" == "201" ]] \
    || { cat "$WORKDIR/adm-evid-$kind.json"; fail "submitting the $kind answered $code"; }
}

evid_submit licence 1
evid_licence_key="$evid_last_key"
evid_licence_id="$(json "$WORKDIR/adm-evid-licence.json" '["id"]')"
evid_submit insurance 1
evid_insurance_id="$(json "$WORKDIR/adm-evid-insurance.json" '["id"]')"

# evid_views — how many access entries name this provider.
evid_views() {
  "$PSQL" "$DATABASE_URL" -tAc \
    "select count(*) from audit_log
      where action = 'verification.evidence_viewed' and target_id = '$verif_first';"
}

evid_before="$(evid_views)"

# --- who may reach it, before anything succeeds -----------------------------------------------------

status="$(curl -s -o "$WORKDIR/adm-evid-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/admin/verifications/$verif_first/documents")"
[[ "$status" == "401" ]] \
  || { cat "$WORKDIR/adm-evid-anon.json"; fail "the viewer answered $status without a credential, want 401"; }

status="$(admin_get "/v1/admin/verifications/$verif_first/documents" "$verif_first_token" evid-usertoken)"
[[ "$status" == "401" ]] \
  || { cat "$WORKDIR/admin-evid-usertoken.json"; fail "a user token reached the viewer and got $status, want 401"; }

# And the provider's own endpoint is still the provider's: the same evidence, on the other
# credential, scoped to the caller by construction and with no identifier anywhere in it.
status="$(curl -s -o "$WORKDIR/adm-evid-own.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $verif_first_token" \
  "http://localhost:$VERIFY_PORT/v1/provider/verification/documents")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/adm-evid-own.json"; fail "the provider could not read their own documents: $status"; }

[[ "$(evid_views)" == "$evid_before" ]] \
  || fail "a refused read, or the provider reading their own file, wrote an administrator access entry"
ok "the evidence is on the administrator credential alone — an anonymous caller and a mobile token are both refused, and neither leaves an access entry"

# --- the images themselves, through a support administrator -----------------------------------------
#
# `support` deliberately: every role holds `verifications.read`, because Docs 01 §4.6 gives "review
# provider verification status" to the least-privileged role and Docs 04 §3 defines that review as
# looking at the images. What makes that defensible is the entry, not the permission.

status="$(admin_get "/v1/admin/verifications/$verif_first/documents" "$verif_sup_token" evid-first)"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/admin-evid-first.json"; fail "a support administrator could not open the file: $status"; }

python3 - "$WORKDIR/admin-evid-first.json" "$evid_licence_id" "$evid_insurance_id" <<'EVIDSHAPE' || fail "the evidence page shape is wrong"
import json, sys
page = json.load(open(sys.argv[1]))
rows = page.get("data")
if rows is None:
    print("data is null"); sys.exit(1)
allowed = {"id", "kind", "content_type", "content_length", "etag",
           "submitted_at", "download_url", "download_expires_at"}
by_id = {}
for entry in rows:
    extra, missing = set(entry) - allowed, allowed - set(entry)
    if extra:
        print("an entry carries", sorted(extra)); sys.exit(1)
    if missing:
        print("an entry is missing", sorted(missing)); sys.exit(1)
    if not entry["download_url"] or not entry["download_expires_at"]:
        print("a document came back with no credential:", entry["kind"]); sys.exit(1)
    if not entry["etag"]:
        print("a document came back with no entity tag:", entry["kind"]); sys.exit(1)
    by_id[entry["id"]] = entry
for want, kind in ((sys.argv[2], "licence"), (sys.argv[3], "insurance")):
    if want not in by_id:
        print("the file does not carry the submitted", kind); sys.exit(1)
    if by_id[want]["kind"] != kind:
        print("the", kind, "came back as", by_id[want]["kind"]); sys.exit(1)
    if by_id[want]["content_length"] != 52:
        print("the stored size is the client's claim rather than the store's answer"); sys.exit(1)
EVIDSHAPE
ok "a reviewer opens a provider's file and gets Docs 04 §3's documents, each with a credential and the store's own size and entity tag"

# The object key never crosses the wire as a field. It is inside the signed URL, because a signature
# over a request names the object it authorises and cannot not — so the claim that is worth making,
# and is made here, is that it appears nowhere the expiry does not reach.
grep -q '"object_key"' "$WORKDIR/admin-evid-first.json" \
  && { cat "$WORKDIR/admin-evid-first.json"; fail "the viewer carries an object_key field"; }
#
# **The stripping is done on the parsed page and not on the raw text**, and the difference is a
# defect this section caught that the Go suite did not: `encoding/json` escapes `&` to `\u0026`,
# a pre-signed URL carries six of them, so replacing the *decoded* URL in the *raw* body matches
# nothing and reports nothing. The domain's fake signed a one-parameter URL with no ampersand and
# passed. Both are fixed; this is the shape that cannot be fooled by an encoding.
python3 - "$WORKDIR/admin-evid-first.json" "$evid_licence_key" <<'EVIDKEY' || fail "the object key outlives the credential beside it"
import json, sys
key = sys.argv[2]
page = json.load(open(sys.argv[1]))
if key not in open(sys.argv[1]).read():
    print("the signed URL does not name the object, so this check would pass vacuously"); sys.exit(1)
if not page["data"]:
    print("the page carries no documents, so nothing is being stripped"); sys.exit(1)
for entry in page["data"]:
    if not entry.get("download_url"):
        print("a document came back with no URL, so nothing is being stripped"); sys.exit(1)
    del entry["download_url"]
if key in json.dumps(page):
    print("the object key survives outside the signed URL"); sys.exit(1)
EVIDKEY
ok "and no durable handle comes with it — the key is inside the signature and nowhere else"

# --- the URL is real, is short-lived, and is minted on the request that asked ------------------------

evid_url_1="$(python3 -c "
import json,sys
for d in json.load(open(sys.argv[1]))['data']:
    if d['id'] == sys.argv[2]: print(d['download_url']); break
" "$WORKDIR/admin-evid-first.json" "$evid_licence_id")"
[[ -n "$evid_url_1" ]] || fail "the licence came back with no download URL"

fetched="$(curl -s -o "$WORKDIR/adm-evid-fetched.bin" -w '%{http_code}' "$evid_url_1")"
[[ "$fetched" == "200" ]] || fail "the administrator's signed download answered $fetched"
[[ "$(cat "$WORKDIR/adm-evid-fetched.bin")" == "not a licence, but exactly fifty-two bytes of proof." ]] \
  || fail "the signed download returned something other than the uploaded bytes"

# The signature is the whole of the authorisation: the same object without one is refused by the
# store, which is what makes "private" a property of the bucket rather than of this endpoint.
unsigned="$(curl -s -o /dev/null -w '%{http_code}' "${evid_url_1%%\?*}")"
[[ "$unsigned" == "403" || "$unsigned" == "401" ]] \
  || fail "the object is readable without a signature (HTTP $unsigned) — the bucket is not private"
ok "the reviewer's URL fetches the image the provider uploaded, and the same object without a signature is refused by the store"

sleep 1
status="$(admin_get "/v1/admin/verifications/$verif_first/documents" "$verif_mod_token" evid-second)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-evid-second.json"; fail "the second read answered $status"; }

evid_url_2="$(python3 -c "
import json,sys
for d in json.load(open(sys.argv[1]))['data']:
    if d['id'] == sys.argv[2]: print(d['download_url']); break
" "$WORKDIR/admin-evid-second.json" "$evid_licence_id")"

[[ "$evid_url_1" != "$evid_url_2" ]] \
  || fail "two reads a second apart produced the same URL, so the credential did not move with the request"
python3 - "$evid_url_1" "$evid_url_2" <<'EVIDFRESH' || fail "both reads carry one signing instant — a stored URL would look exactly like this"
import sys, urllib.parse
def signed_at(u):
    return urllib.parse.parse_qs(urllib.parse.urlsplit(u).query)["X-Amz-Date"][0]
if signed_at(sys.argv[1]) == signed_at(sys.argv[2]):
    sys.exit(1)
EVIDFRESH

# And there is no column a URL could have come out of, which is what makes the freshness above a
# property of the schema rather than of one implementation.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
      "select count(*) from information_schema.columns
        where table_name = 'provider_verification_documents'
          and (column_name like '%url%' or column_name like '%link%');")" == "0" ]] \
  || fail "provider_verification_documents holds a URL-shaped column, so a credential is stored at rest"
ok "each read mints its own signature with its own expiry, and no column exists that a stored one could come from"

# --- the access log, which is the clause most easily met in name only --------------------------------

evid_after="$(evid_views)"
(( evid_after == evid_before + 2 )) \
  || fail "two reads left $((evid_after - evid_before)) access entries, want 2 — an access log that records one look in two is one nobody can rely on"

evid_entries="$("$PSQL" "$DATABASE_URL" -tAc \
  "select json_agg(row_to_json(e))
     from (select actor_type, actor_id::text as actor_id, target_type, target_id::text as target_id, metadata
             from audit_log
            where action = 'verification.evidence_viewed' and target_id = '$verif_first'
            order by id) e;")"

# The support administrator's identifier, taken from the sign-in body rather than from a variable
# the SHIP-153 fixture never set. `admin_signin` stores the whole response, and it names the account
# it issued the session for — which is the same value the access entry must carry.
evid_sup_id="$(json "$WORKDIR/admin-verif-sup.json" '["administrator"]["id"]')"
[[ -n "$evid_sup_id" ]] || fail "the support administrator's sign-in did not name the account"

python3 - "$evid_entries" "$evid_sup_id" "$verif_mod_id" "$evid_licence_id" "$evid_insurance_id" \
  <<'EVIDLOG' || fail "the access entries do not record who looked at whose evidence"
import json, sys
entries = json.loads(sys.argv[1])
support, moderator = sys.argv[2], sys.argv[3]
wanted = {sys.argv[4], sys.argv[5]}
if len(entries) < 2:
    print("only", len(entries), "entries"); sys.exit(1)
first, second = entries[-2], entries[-1]
for entry, who in ((first, support), (second, moderator)):
    if entry["actor_type"] != "admin":
        print("actor_type is", entry["actor_type"]); sys.exit(1)
    if entry["actor_id"] != who:
        print("the entry names", entry["actor_id"], "and the read was made by", who); sys.exit(1)
    if entry["target_type"] != "user":
        print("target_type is", entry["target_type"]); sys.exit(1)
    metadata = entry["metadata"]
    if metadata.get("document_count") != 2:
        print("document_count is", metadata.get("document_count")); sys.exit(1)
    if not wanted.issubset(set(metadata.get("document_ids") or [])):
        print("the entry does not name the images handed over:", metadata.get("document_ids"))
        sys.exit(1)
EVIDLOG
ok "every read names the administrator who made it, the provider whose file it was, and the images handed over — Docs 04 §6.6's evidence reference, in a table nobody can rewrite"

# The trail is append-only, so the record of who was shown somebody's licence cannot be tidied away
# afterwards. `000003`'s trigger, from a psql prompt rather than through the service.
evid_entry_id="$("$PSQL" "$DATABASE_URL" -tAc \
  "select id from audit_log where action = 'verification.evidence_viewed'
     and target_id = '$verif_first' order by id desc limit 1;")"
if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
     "delete from audit_log where id = '$evid_entry_id';" >/dev/null 2>&1; then
  fail "an access entry was deleted — the record of who saw somebody's identity documents is not append-only"
fi
[[ "$(evid_views)" == "$evid_after" ]] || fail "an access entry disappeared"
ok "and an access entry cannot be deleted, from the service or from a database prompt"

# --- an account with no verification record ---------------------------------------------------------
#
# A customer has none — `000200` gives one to providers — so there is no file to open. It is a plain
# 404 because the caller is an administrator holding a permission over verifications and, unlike
# dispute intake, nothing is being kept from them. **And nothing is written**: an entry for a read
# that did not happen would record a review of a file nobody was shown, and would let somebody probe
# which identifiers exist by reading the trail rather than the responses.

status="$(admin_get "/v1/admin/verifications/$verif_customer_id/documents" "$verif_mod_token" evid-cust)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/admin-evid-cust.json"; fail "a customer's evidence answered $status, want 404"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
      "select count(*) from audit_log
        where action = 'verification.evidence_viewed' and target_id = '$verif_customer_id';")" == "0" ]] \
  || fail "a refused read wrote an access entry, which is a trail describing a review that did not happen"

status="$(admin_get "/v1/admin/verifications/not-a-uuid/documents" "$verif_mod_token" evid-badid)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/admin-evid-badid.json"; fail "a malformed identifier answered $status, want 400"; }
ok "an account with no verification record is a plain 404 that writes nothing, and a malformed identifier is a 400"

admin_clear_limits

# ==========================================================================================
# SHIP-159 — the expiry queue: Docs/04 §5's seventh and last.
#
# # What this section demonstrates that no Go test can
#
#   1. **The column is populated by the platform rather than by SQL.** Every document below reaches
#      the queue through `POST /v1/provider/verification/documents` with an `expires_at` on the body
#      — minted, uploaded, submitted. A column nothing in the running system can write is a dead
#      column, and the whole point of this check is that there is a path.
#   2. **The adapter under test is the one that ships.** `expiringDocuments` and
#      `verificationLeadTimes` live in package main, where no test has a database to reach.
#   3. **The configuration is the deployment's.** `VERIFICATION_EXPIRY_LEAD_TIMES` reaches the
#      running service through `deploy/.env`, and this section reads the same value to place its
#      fixtures — so the horizon being asserted is the one the server is actually using.
#
# # The fixtures are placed relative to the configured horizon, and that is deliberate
#
# The lead time has **no default**: Docs/04 §3 gives the renewal cadence to legal and insurance
# advisers (Track-X row X-4) and it is unanswered, so a tree with no `deploy/.env` line runs every
# kind at a horizon of *now*. Rather than skip half the section there — a conditional check is a
# check that can be quietly vacuous — each fixture is written at `horizon ± a margin`, so the same
# four assertions run in both configurations and mean the same thing in both. With a lead time set
# they exercise "expiring ahead of time"; with none they exercise the same boundary at zero.
#
# `internal/profiles/expiry_test.go` drives a configured horizon unconditionally, which is the half
# no harness can guarantee on a machine whose `deploy/.env` it does not own.

ticket "SHIP-159  expiring and expired provider documents surface ahead of time"

admin_clear_limits

# The horizon this deployment applies to an insurance certificate, in seconds, taken from the same
# variable the server was started with. Zero when nothing is configured, which is the shipping
# default and is a number rather than a special case.
expq_lead="$(python3 - "${VERIFICATION_EXPIRY_LEAD_TIMES:-}" <<'EXPLEAD'
import re, sys
units = {"s": 1, "m": 60, "h": 3600}
for pair in sys.argv[1].split(","):
    name, _, raw = pair.partition("=")
    if name.strip() != "insurance" or not raw:
        continue
    total = 0
    for value, unit in re.findall(r"([0-9.]+)([a-z]+)", raw.strip()):
        total += float(value) * units.get(unit, 0)
    print(int(total))
    break
else:
    print(0)
EXPLEAD
)"

# expq_provider <suffix> <name> <phone-digit> — a registered provider, answering with its id.
expq_provider() {
  local out="$WORKDIR/expq-provider-$1.json" code
  code="$(post_json "verify-adm159-reg-$1-$$" /v1/auth/register \
    "{\"name\":\"$2\",\"email\":\"verify-expq-$1-$$@example.com\",\"phone\":\"04193$3$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
    "$out")"
  [[ "$code" == "201" ]] || { cat "$out"; fail "could not register the provider $1: $code"; }
  json "$out" '["id"]'
}

# expq_submit <token> <kind> <n> <expires-at-or-empty> — one document through the platform's own
# three endpoints, leaving its record in $WORKDIR/expq-<n>.json.
#
# A global rather than a printed value, and called directly rather than in a command substitution:
# `fail` exits, and inside `$( … )` that exits only the subshell.
expq_submit() {
  local token="$1" kind="$2" n="$3" expires="$4" url key code body
  code="$(curl -s -X POST -o "$WORKDIR/expq-url-$n.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $token" -H "Idempotency-Key: verify-adm159-url-$n-$$" \
    -H 'Content-Type: application/json' -d '{"content_type":"image/jpeg","content_length":52}' \
    "http://localhost:$VERIFY_PORT/v1/provider/verification/documents/uploads")"
  [[ "$code" == "200" ]] || { cat "$WORKDIR/expq-url-$n.json"; fail "minting a URL for $n answered $code"; }

  key="$(json "$WORKDIR/expq-url-$n.json" '["object_key"]')"
  url="$(json "$WORKDIR/expq-url-$n.json" '["upload_url"]')"

  code="$(curl -s -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: image/jpeg" \
    --data-binary "@$WORKDIR/adm-evidence.bin" "$url")"
  [[ "$code" == "200" ]] || fail "uploading $n answered $code"

  if [[ -n "$expires" ]]; then
    body="{\"kind\":\"$kind\",\"object_key\":\"$key\",\"expires_at\":\"$expires\"}"
  else
    body="{\"kind\":\"$kind\",\"object_key\":\"$key\"}"
  fi

  code="$(curl -s -X POST -o "$WORKDIR/expq-$n.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $token" -H "Idempotency-Key: verify-adm159-rec-$n-$$" \
    -H 'Content-Type: application/json' -d "$body" \
    "http://localhost:$VERIFY_PORT/v1/provider/verification/documents")"
  [[ "$code" == "201" ]] || { cat "$WORKDIR/expq-$n.json"; fail "submitting $n answered $code"; }
}

# expq_at <offset-seconds> — an RFC 3339 instant that far from now, in UTC.
#
# Derived from the same clock the assertions are made against rather than transcribed, because a
# hard-coded time asserts a timezone and this machine is not UTC.
expq_at() {
  python3 -c "
import datetime, sys
at = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(seconds=int(sys.argv[1]))
print(at.strftime('%Y-%m-%dT%H:%M:%SZ'))" "$1"
}

expq_margin=86400

# Three providers, because the three horizon-sensitive fixtures are all **insurance certificates**
# and `000201` is append-only: the newest row of a kind is the current document, so two of them on
# one provider would reduce to one and the assertion would silently be about a different fixture.
# They are all the same kind on purpose — the horizon read below is insurance's, and placing a
# fixture at that boundary while recording it under a different kind would compare two numbers a
# deployment can set independently.
expq_one="$(expq_provider one "Marisol Quintanilha" 1)"
expq_two="$(expq_provider two "Anselm Rutherglen" 2)"
expq_three="$(expq_provider three "Kealoha Brennanmoor" 3)"
expq_one_token="$(mint_token "$expq_one")"
expq_two_token="$(mint_token "$expq_two")"
expq_three_token="$(mint_token "$expq_three")"

# Lapsed a month ago: on the queue under every configuration, and the reason the "expired" half
# needs no answer from X-4 at all.
expq_submit "$expq_one_token" insurance lapsed "$(expq_at -2592000)"
expq_lapsed_id="$(json "$WORKDIR/expq-lapsed.json" '["id"]')"

# Just inside this deployment's horizon for an insurance certificate. With a lead time configured
# this is a document that has *not* lapsed and is nonetheless due; with none it is one that lapsed a
# day ago. Both are the same assertion about the same boundary.
expq_submit "$expq_two_token" insurance inside "$(expq_at $((expq_lead - expq_margin)))"
expq_inside_id="$(json "$WORKDIR/expq-inside.json" '["id"]')"

# Just outside it, which must not be on the queue in either configuration.
expq_submit "$expq_three_token" insurance outside "$(expq_at $((expq_lead + expq_margin)))"
expq_outside_id="$(json "$WORKDIR/expq-outside.json" '["id"]')"

# States no expiry at all: never on the queue, because NULL means the platform was never told.
expq_submit "$expq_one_token" abn_evidence silent ""
expq_silent_id="$(json "$WORKDIR/expq-silent.json" '["id"]')"

# A retake: the superseded row lapsed last week and the current one does not lapse for years. Only
# the current document of a kind is on the queue — `000201` is append-only and nothing can delete the
# old row, so a queue that read every row would chase a renewal that has already happened, for ever.
expq_submit "$expq_one_token" licence superseded "$(expq_at -604800)"
expq_superseded_id="$(json "$WORKDIR/expq-superseded.json" '["id"]')"
# A century out, rather than a decade: this is the one fixture whose absence depends on no lead
# time being longer than the gap, and a licence horizon nobody would configure is cheaper than a
# second variable to read.
expq_submit "$expq_one_token" licence current "$(expq_at 3153600000)"
expq_current_id="$(json "$WORKDIR/expq-current.json" '["id"]')"

# --- who may read it -----------------------------------------------------------------------------

status="$(curl -s -o "$WORKDIR/expq-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/admin/moderation/expiring-documents")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/expq-anon.json"; fail "the queue answered $status without a credential, want 401"; }

status="$(admin_get "/v1/admin/moderation/expiring-documents" "$expq_one_token" expq-usertoken)"
[[ "$status" == "401" ]] \
  || { cat "$WORKDIR/admin-expq-usertoken.json"; fail "a user token reached the queue and got $status, want 401"; }
ok "the expiry queue is on the administrator credential alone — an anonymous caller and a mobile token are both refused"

# --- the queue itself ----------------------------------------------------------------------------

status="$(admin_get "/v1/admin/moderation/expiring-documents?limit=100" "$verif_sup_token" expq-page)"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/admin-expq-page.json"; fail "a support administrator could not read the queue: $status"; }

python3 - "$WORKDIR/admin-expq-page.json" "$expq_lapsed_id" "$expq_inside_id" "$expq_outside_id" \
  "$expq_silent_id" "$expq_superseded_id" "$expq_current_id" "$expq_lead" \
  <<'EXPQPAGE' || fail "the expiry queue does not surface the right documents"
import json, sys
page = json.load(open(sys.argv[1]))
lapsed, inside, outside, silent, superseded, current = sys.argv[2:8]
lead = int(sys.argv[8])

rows = page.get("data")
if rows is None:
    print("data is null"); sys.exit(1)
by_id = {r["document_id"]: r for r in rows}

if lapsed not in by_id:
    print("a document that lapsed a month ago is not on the queue"); sys.exit(1)
if not by_id[lapsed]["expired"]:
    print("the lapsed document does not read as expired"); sys.exit(1)

if inside not in by_id:
    print("a document inside this deployment's horizon is not on the queue"); sys.exit(1)
if lead > 0 and by_id[inside]["expired"]:
    print("a document that has not lapsed reads as expired, so the two halves of the queue "
          "cannot be told apart"); sys.exit(1)

for absent, why in ((outside, "beyond every configured horizon"),
                    (silent, "stating no expiry at all"),
                    (superseded, "superseded by a later submission of the same kind")):
    if absent in by_id:
        print("a document", why, "is on the queue"); sys.exit(1)
if current in by_id:
    print("the current licence is on the queue and does not lapse for ten years"); sys.exit(1)

# Soonest first: the entry that has been out of date longest is at the top.
#
# Parsed rather than compared as strings. PostgreSQL renders a `timestamptz` in the session's
# zone, so a page whose rows carried two different offsets would sort correctly by instant and
# incorrectly by text — and this machine is not UTC.
import datetime
def instant(value):
    return datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
dates = [instant(r["expires_at"]) for r in rows]
if dates != sorted(dates):
    print("the queue is not soonest first:", [d.isoformat() for d in dates[:5]]); sys.exit(1)

# The horizons the query used, so an empty "expiring" half can be told from an unconfigured one.
if page.get("lead_times") is None:
    print("lead_times is null"); sys.exit(1)
if lead > 0 and page["lead_times"].get("insurance") != lead:
    print("lead_times[insurance] is", page["lead_times"].get("insurance"), "and the deployment "
          "was configured with", lead); sys.exit(1)
EXPQPAGE
ok "expired documents and documents inside this deployment's horizon are both on the queue, soonest first — and one beyond it, one stating no expiry, and one superseded by a retake are on none of it"

# The entry's shape: ten keys, nothing commercial, and no credential. A reviewer who wants to look
# at the image opens SHIP-155's viewer, which writes an access entry; a URL here would make every
# load of this queue an unlogged read of everybody's identity documents at once.
python3 - "$WORKDIR/admin-expq-page.json" "$expq_lapsed_id" <<'EXPQSHAPE' || fail "the expiry queue entry shape is wrong"
import json, sys
page = json.load(open(sys.argv[1]))
entry = next(r for r in page["data"] if r["document_id"] == sys.argv[2])
allowed = {"document_id", "provider_id", "name", "email", "phone",
           "verification_state", "kind", "expires_at", "expired", "submitted_at"}
extra, missing = set(entry) - allowed, allowed - set(entry)
if extra:
    print("the entry carries", sorted(extra)); sys.exit(1)
if missing:
    print("the entry is missing", sorted(missing)); sys.exit(1)
if entry["name"] != "Marisol Quintanilha":
    print("the entry does not name the provider:", entry["name"]); sys.exit(1)
if entry["kind"] != "insurance":
    print("the entry does not name the document:", entry["kind"]); sys.exit(1)
if entry["verification_state"] != "Pending":
    print("the entry does not carry the provider's standing:", entry["verification_state"])
    sys.exit(1)
EXPQSHAPE
ok "an entry names the provider, the document and their standing — ten keys, no shape to put a budget in, and no credential to the image"

# --- paging, and that reading the queue writes nothing --------------------------------------------

status="$(admin_get "/v1/admin/moderation/expiring-documents?limit=1" "$verif_mod_token" expq-first)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-expq-first.json"; fail "a page of one answered $status"; }
expq_cursor="$(json "$WORKDIR/admin-expq-first.json" '["next_cursor"]')"
[[ -n "$expq_cursor" ]] || { cat "$WORKDIR/admin-expq-first.json"; fail "a page of one carried no cursor and there is more than one document due"; }

status="$(admin_get "/v1/admin/moderation/expiring-documents?limit=1&cursor=$expq_cursor" "$verif_mod_token" expq-second)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-expq-second.json"; fail "the second page answered $status"; }
[[ "$(json "$WORKDIR/admin-expq-first.json" '["data"][0]["document_id"]')" \
   != "$(json "$WORKDIR/admin-expq-second.json" '["data"][0]["document_id"]')" ]] \
  || fail "the cursor returned the same document twice — a queue that repeats a row is one that skips another"

status="$(admin_get "/v1/admin/moderation/expiring-documents?cursor=not-a-cursor" "$verif_mod_token" expq-badcursor)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/admin-expq-badcursor.json"; fail "a cursor this endpoint never issued answered $status, want 400"; }

expq_audit_before="$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from audit_log;")"
status="$(admin_get "/v1/admin/moderation/expiring-documents?limit=100" "$verif_sup_token" expq-again)"
[[ "$status" == "200" ]] || fail "re-reading the queue answered $status"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from audit_log;")" == "$expq_audit_before" ]] \
  || fail "reading the expiry queue wrote an audit entry — an entry per queue load buries the actions in the reads"
ok "the queue pages without repeating a row, refuses a cursor it never issued, and writes nothing — unlike the evidence viewer one queue over, which hands over a credential"

admin_clear_limits

admin_clear_limits

# ==========================================================================================
# SHIP-164 — the dispute workflow: investigation, and the outcome that unfreezes the job.
#
# # What this section demonstrates that no Go test can
#
# Three things, and each of them is a seam `internal/admin` cannot see from inside.
#
#   1. **The transition runs through cmd/api's real adapter.** `disputeLifecycle.resolve` is in
#      package main, where no test has a database to reach; the Go suite exercises a *copy* of it.
#      The adapter under test here is the one that ships.
#   2. **The two credential systems stay apart.** A user token must not reach the console's dispute
#      endpoints, and the complainant's own intake response must not acquire an administrator's
#      outcome. Both are claims about two packages at once.
#   3. **The freeze and the unfreeze are the same job row.** The SHIP-163 section above proved a
#      dispute freezes it; this proves an outcome lets it move again, on the same fixtures.
#
# # The fixtures are the section's own, and the prefixes above are already taken
#
# 0419 is admin's, and 04190…04198 are spoken for (see the block at the head of this file). This
# section reuses `dispute_customer_id` and `dispute_provider_id` rather than registering more, which
# is what `dispute_delivered_job` was written to make cheap — every job below is a fresh delivery
# between the same two accounts, and the assertions fence on job and dispute identifiers.

ticket "SHIP-164  a dispute moves through investigation to a documented outcome that unfreezes the job"

admin_clear_limits

# A moderator and a support administrator of this section's own, for the reason the SHIP-153 section
# gives: `disputes.read` and `disputes.resolve` split across those two roles is exactly what this
# section is about, and a token borrowed from another section is one another section can revoke.
res_mod_email="verify-admin-res-mod-$$@example.com"
res_mod_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$res_mod_email', 'Verify Resolver', '$admin_fixture_hash', 'moderator')
   returning id;")"
[[ -n "$res_mod_id" ]] || fail "the resolving moderator could not be created"

status="$(admin_signin "verify-adm164-modin-$$" "$res_mod_email" "$admin_password" res-mod)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-res-mod.json"; fail "the resolving moderator could not sign in ($status)"; }
res_mod_token="$(json "$WORKDIR/admin-res-mod.json" '["token"]')"

res_sup_email="verify-admin-res-sup-$$@example.com"
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into admin_users (id, email, name, password_hash, role)
   values (gen_random_uuid(), '$res_sup_email', 'Verify Reader', '$admin_fixture_hash', 'support');" >/dev/null \
  || fail "the support administrator could not be created"
status="$(admin_signin "verify-adm164-supin-$$" "$res_sup_email" "$admin_password" res-sup)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-res-sup.json"; fail "the support administrator could not sign in ($status)"; }
res_sup_token="$(json "$WORKDIR/admin-res-sup.json" '["token"]')"

# res_disputed <label> — a delivered job frozen by an open dispute.
#
# Raised through the endpoint rather than inserted, so the job is frozen the way a complainant
# freezes it.
#
# **It prints `<dispute-id> <job-id>` and callers `read` both**, rather than leaving the job in a
# global. Every call site is a command substitution, which is a subshell, so a variable this function
# assigned would be lost the moment it returned — and under the runner's `set -u` the caller reading
# it aborts the whole section rather than failing a check. That cost this section a run.
res_disputed() {
  local job occurred code
  job="$(dispute_delivered_job "res-$1")"
  occurred="$("$PSQL" "$DATABASE_URL" -tAc \
    "select to_char((now() at time zone 'utc') - interval '3 hours', 'YYYY-MM-DD\"T\"HH24:MI:SS') || 'Z';")"
  code="$(dispute_request "$dispute_customer_token" "verify-adm164-raise-$1-$$" "$job" \
    "{\"category\":\"goods_damaged_or_missing\",\"description\":\"Two of the four crates arrived with the sides staved in.\",\"desired_outcome\":\"A record of the damage.\",\"occurred_at\":\"$occurred\",\"evidence\":[\"Photographed the crates at the depot\"]}" \
    "res-$1")"
  [[ "$code" == "201" ]] || { cat "$WORKDIR/dsp-res-$1.json"; fail "could not raise the dispute for $1: $code"; }
  [[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$job';")" == "Disputed" ]] \
    || fail "the fixture job for $1 is not frozen, so nothing below is testing what it claims"
  printf '%s %s' "$(json "$WORKDIR/dsp-res-$1.json" '["id"]')" "$job"
}

# res_resolve <token> <key> <dispute-id> <body> <name> — one resolution, answering with its status.
res_resolve() {
  admin_post "/v1/admin/disputes/$3/resolution" "$1" "$2" "$4" "$5"
}

res_reason="Reviewed the proof of delivery and the messages between the parties before deciding."

# --- the queue: an administrator cannot investigate a dispute they cannot find --------------------

read -r res_first res_first_job <<< "$(res_disputed first)"

status="$(curl -s -o "$WORKDIR/res-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/admin/disputes")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/res-anon.json"; fail "the dispute queue answered $status without a credential, want 401"; }

status="$(admin_get "/v1/admin/disputes" "$dispute_customer_token" res-userq)"
[[ "$status" == "401" ]] \
  || { cat "$WORKDIR/admin-res-userq.json"; fail "a user token reached the dispute queue and got $status, want 401"; }
ok "the dispute queue is on the administrator credential alone — a mobile token cannot reach it, which is the two systems staying apart"

status="$(admin_get "/v1/admin/disputes?limit=100" "$res_sup_token" res-queue)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-res-queue.json"; fail "a support administrator could not read the dispute queue: $status"; }

# res_carries <name> <dispute-id> — how many times the page names that dispute.
res_carries() {
  python3 - "$WORKDIR/admin-$1.json" "$2" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
print(sum(1 for e in (page.get("data") or []) if e["id"] == sys.argv[2]))
PY
}

[[ "$(res_carries res-queue "$res_first")" == "1" ]] \
  || { cat "$WORKDIR/admin-res-queue.json"; fail "the open dispute is not on Docs 04 §5's sixth queue"; }
ok "an open dispute appears on the queue with no state parameter — Docs 04 §5 names it 'open disputes', so that is what the default means"

# The entry's key set, closed. A budget arrives on an administrative shape as a field somebody added,
# and SHIP-83 established that a spelling-based guard misses one named anything at all.
python3 - "$WORKDIR/admin-res-queue.json" "$res_first" <<'PY' || fail "the dispute queue entry shape is wrong"
import json, sys
page = json.load(open(sys.argv[1]))
entry = next(e for e in page["data"] if e["id"] == sys.argv[2])
allowed = {"id", "job_id", "complainant_id", "complainant_party", "category",
           "occurred_at", "raised_at", "resolved_at", "outcome", "resolved_by"}
extra, missing = set(entry) - allowed, allowed - set(entry)
if extra:
    print("the entry carries", sorted(extra)); sys.exit(1)
if missing:
    print("the entry is missing", sorted(missing)); sys.exit(1)
if entry["complainant_party"] != "customer":
    print("complainant_party is", entry["complainant_party"]); sys.exit(1)
if entry["resolved_at"] or entry["outcome"] or entry["resolved_by"]:
    print("an open dispute carries resolution fields:", entry); sys.exit(1)
if "description" in entry or "evidence" in entry:
    print("the queue carries the account; that is the detail endpoint's"); sys.exit(1)
PY
ok "a queue entry is ten keys, carries nothing commercial and no shape to put a budget in, and its resolution fields are empty while it is open"

status="$(admin_get "/v1/admin/disputes?state=pending" "$res_mod_token" res-badstate)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/admin-res-badstate.json"; fail "a queue half that does not exist returned $status, want 422"; }
[[ "$(cat "$WORKDIR/admin-res-badstate.json")" == *'"state"'* ]] \
  || { cat "$WORKDIR/admin-res-badstate.json"; fail "the refusal does not name the field"; }
ok "a state that is neither open nor resolved is refused rather than ignored — an ignored filter answers something that looks like an answer"

# --- investigation: the complaint itself, which was reachable from nowhere ------------------------

status="$(admin_get "/v1/admin/disputes/$res_first" "$res_sup_token" res-detail)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-res-detail.json"; fail "a support administrator could not open a dispute: $status"; }
python3 - "$WORKDIR/admin-res-detail.json" <<'PY' || fail "the dispute detail shape is wrong"
import json, sys
d = json.load(open(sys.argv[1]))
for want in ("description", "desired_outcome", "evidence"):
    if want not in d:
        print("the dispute does not carry", want); sys.exit(1)
if d["description"] != "Two of the four crates arrived with the sides staved in.":
    print("description is", d["description"]); sys.exit(1)
if d["evidence"] != ["Photographed the crates at the depot"]:
    print("evidence is", d["evidence"]); sys.exit(1)
for forbidden in ("idempotency_key", "key"):
    if forbidden in d:
        print("the dispute carries", forbidden); sys.exit(1)
PY
ok "Docs 04 §7's investigation read: the account, the desired outcome and the evidence, and never the complainant's idempotency key"

status="$(admin_get "/v1/admin/disputes/00000000-0000-7000-8000-000000000000" "$res_mod_token" res-nodispute)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/admin-res-nodispute.json"; fail "a dispute that does not exist returned $status, want 404"; }
ok "a dispute that does not exist is a plain 404 — the caller is an administrator, and unlike intake nothing is being kept from them"

# --- the permission split, and that a refused resolution changes nothing --------------------------

status="$(res_resolve "$res_sup_token" "verify-adm164-sup-$$" "$res_first" \
  "{\"outcome\":\"delivery_completed_as_agreed\",\"job_outcome\":\"completed\",\"reason\":\"$res_reason\"}" res-supres)"
[[ "$status" == "403" ]] || { cat "$WORKDIR/admin-res-supres.json"; fail "a support administrator resolved a dispute and got $status, want 403"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$res_first_job';")" == "Disputed" ]] \
  || fail "a refused resolution unfroze the job anyway"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc "select count(*) from disputes where id = '$res_first' and resolved_at is not null;")" == "0" ]] \
  || fail "a refused resolution settled the dispute anyway"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc "select count(*) from audit_log where target_id = '$res_first_job' and action = 'dispute.resolved';")" == "0" ]] \
  || fail "a refused resolution wrote an audit entry, in a table nothing can correct"
ok "a support administrator may investigate a dispute and may not settle one — Docs 04 §9's least privilege as two permissions on two endpoints, and the refusal left three tables untouched"

# --- the outcome, and the unfreeze this ticket's *Done when* is about ------------------------------

status="$(res_resolve "$res_mod_token" "verify-adm164-badout-$$" "$res_first" \
  "{\"outcome\":\"compensation_awarded\",\"job_outcome\":\"completed\",\"reason\":\"$res_reason\"}" res-badout)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/admin-res-badout.json"; fail "an outcome Docs 04 §7 does not list returned $status, want 422"; }
[[ "$(cat "$WORKDIR/admin-res-badout.json")" == *'"outcome"'* ]] || fail "the refusal does not name the outcome field"

status="$(res_resolve "$res_mod_token" "verify-adm164-badjob-$$" "$res_first" \
  "{\"outcome\":\"failed_delivery_recorded\",\"job_outcome\":\"Disputed\",\"reason\":\"$res_reason\"}" res-badjob)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/admin-res-badjob.json"; fail "a destination Docs 02 §2 does not offer returned $status, want 422"; }
[[ "$(cat "$WORKDIR/admin-res-badjob.json")" == *'"job_outcome"'* ]] || fail "the refusal does not name the job_outcome field"
ok "the two vocabularies are refused separately and each refusal names its own field — a console sending a good outcome with a bad destination is told which half is wrong"

status="$(res_resolve "$res_mod_token" "verify-adm164-first-$$" "$res_first" \
  "{\"outcome\":\"delivery_completed_as_agreed\",\"job_outcome\":\"completed\",\"reason\":\"$res_reason\"}" res-done)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-res-done.json"; fail "resolving a dispute returned $status, want 200"; }

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$res_first_job';")" == "Completed" ]] \
  || fail "the job is still frozen after its dispute was resolved — the *Done when* is an outcome that unfreezes it"
ok "the outcome unfroze the job: Disputed to Completed, through the one guarded transition (Docs 02 §2)"

# The row, read rather than the answer the endpoint gave about itself. All three columns, because
# ck_disputes_resolution binds them and a dispute with resolved_at alone is off idx_disputes_open
# with nothing documented about it.
res_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (resolved_at is not null) || '|' || outcome || '|' || (resolved_by = '$res_mod_id')
     from disputes where id = '$res_first';")"
[[ "$res_row" == "true|Delivery completed as agreed|true" ]] \
  || fail "the stored resolution is '$res_row', want 'true|Delivery completed as agreed|true'"
ok "the outcome is documented on the row in Docs 04 §7's own vocabulary, with the administrator who recorded it"

# The reason is in both tables, and neither is redundant: job_status_history is what a customer's
# support conversation reads and audit_log is what Docs 04 §9's controls read.
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from job_status_history
    where job_id = '$res_first_job' and to_status = 'Completed'
      and actor_type = 'admin' and reason = '$res_reason';")" == "1" ]] \
  || fail "the administrator's reason is not on the job's status history"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log
    where target_id = '$res_first_job' and target_type = 'job'
      and action = 'dispute.resolved' and actor_id = '$res_mod_id'
      and reason = '$res_reason'
      and metadata->>'dispute_id' = '$res_first'
      and metadata->>'outcome' = 'Delivery completed as agreed'
      and metadata->>'job_outcome' = 'completed';")" == "1" ]] \
  || fail "the audit entry does not carry the administrator, the dispute and both vocabularies"
ok "the reason reached the job's history and the audit trail, and the entry names the job with the dispute and both vocabularies in its metadata"

# The queue moved it. idx_disputes_open is partial on `resolved_at IS NULL`, so a queue reading the
# whole table would have looked correct until the first resolution.
status="$(admin_get "/v1/admin/disputes?state=open&limit=100" "$res_mod_token" res-openq)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-res-openq.json"; fail "the open queue returned $status"; }
[[ "$(res_carries res-openq "$res_first")" == "0" ]] || fail "a resolved dispute is still on the open queue"
status="$(admin_get "/v1/admin/disputes?state=resolved&limit=100" "$res_mod_token" res-resq)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-res-resq.json"; fail "the resolved queue returned $status"; }
[[ "$(res_carries res-resq "$res_first")" == "1" ]] || fail "a resolved dispute is not on the resolved queue"
ok "resolving moves the dispute between the two halves of the queue — the open one is a partial index, not a filter somebody remembered"

# --- the events, fenced by id, because one broker serves every worktree ---------------------------
#
# No new aggregate and no new topic: the guarded transition emits `job.status_changed`, which
# notifications.StatusRules already routes to the customer and the awarded provider on Completed and
# Cancelled. This asserts the outbox row exists for *this* job — a count over the topic would see
# every other worktree's run as readily as its own.

[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from outbox
    where aggregate_id = '$res_first_job' and event_type = 'job.status_changed'
      and payload->>'to' = 'Completed';")" == "1" ]] \
  || fail "resolving the dispute emitted no job.status_changed, so nothing notifies the parties"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from outbox where aggregate_id = '$res_first' or event_type like 'dispute.%';")" == "0" ]] \
  || fail "a dispute-shaped event was emitted; SHIP-164 adds no fourth aggregate and no new topic"
ok "the unfreeze rides the existing job.status_changed and emits nothing of its own — one state change, one announcement"

# --- a second resolution, and a job that has moved on ---------------------------------------------

status="$(res_resolve "$res_mod_token" "verify-adm164-twice-$$" "$res_first" \
  "{\"outcome\":\"failed_delivery_recorded\",\"job_outcome\":\"cancelled\",\"reason\":\"$res_reason\"}" res-twice)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/admin-res-twice.json"; fail "a second resolution returned $status, want 409"; }
[[ "$(json "$WORKDIR/admin-res-twice.json" '["error"]["code"]')" == "admin_dispute_already_resolved" ]] \
  || { cat "$WORKDIR/admin-res-twice.json"; fail "the refusal does not carry admin_dispute_already_resolved"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select outcome from disputes where id = '$res_first';")" == "Delivery completed as agreed" ]] \
  || fail "a refused second resolution overwrote the first administrator's finding"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$res_first_job';")" == "Completed" ]] \
  || fail "a refused second resolution moved the job again"
ok "a second resolution is refused and the first administrator's finding stands — on a queue two moderators are reading, that is the answer the second one gets"

# --- the other destination, and that the two vocabularies are independent -------------------------
#
# `delivery_issue_acknowledged` on a job that nevertheless **completes** is the pairing a derivation
# would refuse, and 000804's whole argument is that three of Docs 04 §7's five outcomes are equally
# true of a delivery that completed and one that failed. A resolution deriving the destination from
# the finding cannot record this at all.

read -r res_second res_second_job <<< "$(res_disputed second)"

status="$(res_resolve "$res_mod_token" "verify-adm164-orth-$$" "$res_second" \
  "{\"outcome\":\"delivery_issue_acknowledged\",\"job_outcome\":\"completed\",\"reason\":\"Damage acknowledged; the parties are settling it between themselves.\"}" res-orth)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-res-orth.json"; fail "an acknowledged issue on a completed job returned $status, want 200"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select outcome || '|' || (select status from jobs where id = '$res_second_job') from disputes where id = '$res_second';")" \
  == "Delivery issue acknowledged|Completed" ]] \
  || fail "the outcome and the job's destination did not stay independent"
ok "Docs 04 §7's finding and Docs 02 §2's destination are orthogonal — an acknowledged issue on a job the customer nonetheless accepts is a pairing a derived destination could not record"

read -r res_third res_third_job <<< "$(res_disputed third)"

status="$(res_resolve "$res_mod_token" "verify-adm164-cancel-$$" "$res_third" \
  "{\"outcome\":\"failed_delivery_recorded\",\"job_outcome\":\"cancelled\",\"reason\":\"Two crates damaged beyond use; the provider accepts the delivery failed.\"}" res-cancel)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/admin-res-cancel.json"; fail "resolving as cancelled returned $status, want 200"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$res_third_job';")" == "Cancelled" ]] \
  || fail "the job did not move to Cancelled"
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from outbox
    where aggregate_id = '$res_third_job' and event_type = 'job.status_changed'
      and payload->>'to' = 'Cancelled';")" == "1" ]] \
  || fail "the cancellation emitted no job.status_changed"
ok "both of Docs 02 §2's rows out of Disputed are reachable, each through its own method on the lifecycle port — and neither is the unpublish route, which cannot touch an awarded job at all"

# --- a job that moved out from under its dispute ---------------------------------------------------
#
# The other half of the drift: SHIP-163's service.go warns of a job carrying an open dispute while
# not being Disputed, and an administrator meeting it needs telling plainly rather than a 500.

read -r res_fourth res_fourth_job <<< "$(res_disputed fourth)"

"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -v job="$res_fourth_job" -v actor="$res_mod_id" \
  >/dev/null <<'SQL'
BEGIN;
WITH written AS (
    INSERT INTO job_status_history
        (id, job_id, from_status, to_status, actor_type, actor_id, reason, actor_recorded_at)
    VALUES (gen_random_uuid(), :'job', 'Disputed', 'Cancelled', 'admin', :'actor',
            'Cancelled under a different dispute on the same delivery.', now())
    RETURNING id
)
SELECT set_config('shipper.job_status_transition', (SELECT id::text FROM written), true);
UPDATE jobs SET status = 'Cancelled' WHERE id = :'job';
COMMIT;
SQL

status="$(res_resolve "$res_mod_token" "verify-adm164-moved-$$" "$res_fourth" \
  "{\"outcome\":\"failed_delivery_recorded\",\"job_outcome\":\"cancelled\",\"reason\":\"$res_reason\"}" res-moved)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/admin-res-moved.json"; fail "resolving a job that had moved on returned $status, want 409"; }
[[ "$(json "$WORKDIR/admin-res-moved.json" '["error"]["code"]')" == "admin_job_not_resolvable" ]] \
  || { cat "$WORKDIR/admin-res-moved.json"; fail "the refusal does not carry admin_job_not_resolvable"; }
[[ "$("$PSQL" "$DATABASE_URL" -qtAc "select count(*) from disputes where id = '$res_fourth' and resolved_at is not null;")" == "0" ]] \
  || fail "the dispute was settled against a job that had already moved — the exact corrupt state service.go warns about"
ok "a job that has moved out from under its dispute is a 409 a console can act on, and the refusal inside the transaction left the dispute open"

# --- the complainant's own view is unchanged, which is the two shapes staying apart ---------------

status="$(dispute_request "$dispute_customer_token" "verify-adm164-replay-$$" "$res_first_job" \
  "{\"category\":\"goods_damaged_or_missing\",\"description\":\"x\",\"desired_outcome\":\"y\",\"occurred_at\":\"2026-01-01T00:00:00Z\"}" res-reraise)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/dsp-res-reraise.json"; fail "raising a dispute on a completed job returned $status, want 409"; }
[[ "$(json "$WORKDIR/dsp-res-reraise.json" '["error"]["code"]')" == "admin_job_not_disputable" ]] \
  || { cat "$WORKDIR/dsp-res-reraise.json"; fail "expected admin_job_not_disputable once the job has completed"; }
ok "once the outcome has completed the job it can no longer be disputed — the guard refuses it, which is the freeze being genuinely lifted rather than a flag being cleared"

admin_clear_limits
