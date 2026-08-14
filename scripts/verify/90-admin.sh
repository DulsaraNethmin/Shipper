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

# ==========================================================================================
# SHIP-147 — administrator sign-in, and the two directions it must not be reachable in.
#
# # This section rate-limits, and it clears its own keys at both ends
#
# `make verify` runs every request from 127.0.0.1, so the per-address bucket administrator sign-in
# uses is shared with every other section and with every concurrent worktree. The keys are
# `rl:v1:admin-signin:*` — a different namespace from identity's `rl:v1:signin:*`, because an
# administrator's failed attempts and a user's are separate allowances — and they are deleted before
# the first sign-in and after the last, exactly as 40-identity.sh does with its own.
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

admin_clear_limits() {
  redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:admin-signin:*' \
    | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true
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
[[ "$(redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:admin-signin:*' | wc -l | tr -d ' ')" == "0" ]] \
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
INSERT INTO milestones (id, job_id, milestone, actor_type, actor_id, reason, actor_recorded_at)
VALUES (gen_random_uuid(), :'job', 'Delivered', 'driver', gen_random_uuid(),
        'The recipient asked me not to photograph their door.', now());

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
found = next((i for i in items if i["proof_id"] == sys.argv[2]), None)
photo = any(i["proof_id"] == sys.argv[3] for i in items)
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
