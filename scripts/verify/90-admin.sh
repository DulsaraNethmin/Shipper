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
status="$(post_json "verify-adm-search-reg-$$" /v1/auth/register \
  "{\"email\":\"$search_email\",\"phone\":\"$search_phone\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/search-user.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/search-user.json"; fail "could not register the searchable customer: $status"; }
search_user_id="$(json "$WORKDIR/search-user.json" '["id"]')"

suspended_email="verify-search-gone-$$@example.com"
status="$(post_json "verify-adm-search-susp-$$" /v1/auth/register \
  "{\"email\":\"$suspended_email\",\"phone\":\"04194$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
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
[[ "$search_keys" == "created_at,email,email_verified_at,id,phone,phone_verified_at,role,status" ]] \
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
# `admin_clear_limits` above clears `rl:v1:admin-signin:*`, which is a separate keyspace — an
# administrator's failures and a user's against one address are separate allowances (see
# credentials.go). This clears the user one.
redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:signin:*' \
  | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true

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
  "{\"email\":\"$stand_email\",\"phone\":\"04195$$\",\"password\":\"$stand_password\",\"role\":\"provider\"}" \
  "$WORKDIR/standing-user.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/standing-user.json"; fail "could not register the account: $status"; }
stand_user_id="$(json "$WORKDIR/standing-user.json" '["id"]')"

# --- the permission ---------------------------------------------------------------------------------

stand_reason="Two unresolved no-shows in a fortnight; see the delivery exception queue."
stand_body="{\"standing\":\"suspended\",\"reason\":\"$stand_reason\"}"

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
  '{"standing":"suspended","reason":"bad"}' noreason)"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/stand-noreason.json"; fail "a reason that records nothing returned $status, want 422"; }
ok "a standing outside ck_users_status's three and a reason too short to record anything are both refused and name their field"

# --- the account can use the platform, and then cannot ------------------------------------------------
#
# **This is the pair no Go test in internal/admin can make.** The domain writes a column; whether a
# suspended account can still sign in is internal/identity's code, and the two only meet in the
# running service.

status="$(post_json "verify-adm161-login1-$$" /v1/auth/login \
  "{\"email\":\"$stand_email\",\"password\":\"$stand_password\",\"device_label\":\"Verify Standing\"}" "$WORKDIR/stand-login-before.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/stand-login-before.json"; fail "the account could not sign in before being suspended ($status)"; }
stand_refresh="$(json "$WORKDIR/stand-login-before.json" '["refresh_token"]')"
ok "the account signs in while it is active, which is the precondition the next check needs"

status="$(standing "$unpub_token" "verify-adm161-suspend-$$" "$stand_user_id" "$stand_body" susp)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/stand-susp.json"; fail "suspending returned $status, want 200"; }
[[ "$(json "$WORKDIR/stand-susp.json" '["from"]')" == "active" ]] \
  || { cat "$WORKDIR/stand-susp.json"; fail "the response does not say what the account held before"; }
[[ "$(json "$WORKDIR/stand-susp.json" '["to"]')" == "suspended" ]] \
  || { cat "$WORKDIR/stand-susp.json"; fail "the response does not say what the account holds now"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from users where id = '$stand_user_id';")" == "suspended" ]] \
  || fail "the account is not suspended"
ok "an account is suspended, and the response carries both ends — a console rendering only the new standing cannot tell a tightening from a loosening"

[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log
    where target_id = '$stand_user_id' and target_type = 'user'
      and action = 'user.standing_changed' and actor_id = '$unpub_id'
      and reason = '$stand_reason'
      and metadata->>'from' = 'active' and metadata->>'to' = 'suspended';")" == "1" ]] \
  || fail "the change wrote no audit entry carrying the reason and both ends"
ok "and the audit entry names the account, the administrator, the reason and both ends of the change"

# The enforcement. Sign-in is refused, and so is refresh — which is where a suspension actually takes
# effect, because an access token lives fifteen minutes and carries no standing.
status="$(post_json "verify-adm161-login2-$$" /v1/auth/login \
  "{\"email\":\"$stand_email\",\"password\":\"$stand_password\",\"device_label\":\"Verify Standing\"}" "$WORKDIR/stand-login-after.json")"
[[ "$status" == "403" ]] \
  || { cat "$WORKDIR/stand-login-after.json"; fail "a suspended account signed in and got $status, want 403"; }

status="$(post_json "verify-adm161-refresh-$$" /v1/auth/refresh \
  "{\"refresh_token\":\"$stand_refresh\"}" "$WORKDIR/stand-refresh.json")"
[[ "$status" != "200" ]] \
  || { cat "$WORKDIR/stand-refresh.json"; fail "a suspended account refreshed its session, so the suspension does not take effect until the token expires"; }
ok "the suspended account can no longer sign in, and the session it already had cannot be refreshed — 'account access is disabled', on the wire"

# --- a no-op, and putting the account back ------------------------------------------------------------

status="$(standing "$unpub_token" "verify-adm161-noop-$$" "$stand_user_id" "$stand_body" noop)"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/stand-noop.json"; fail "setting the standing it already holds returned $status, want 409"; }
[[ "$(cat "$WORKDIR/stand-noop.json")" == *'admin_user_standing_unchanged'* ]] \
  || { cat "$WORKDIR/stand-noop.json"; fail "the refusal does not carry the code a console branches on"; }
[[ "$("$PSQL" "$DATABASE_URL" -qtAc \
  "select count(*) from audit_log where target_id = '$stand_user_id';")" == "1" ]] \
  || fail "a no-op wrote a second entry, in a table whose value is that everything in it happened"
ok "setting the standing an account already holds is refused rather than recorded — an entry saying suspended to suspended is noise in the one table that must be all signal"

status="$(standing "$unpub_token" "verify-adm161-reinstate-$$" "$stand_user_id" \
  '{"standing":"active","reason":"No-shows explained and evidenced; access restored after review."}' back)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/stand-back.json"; fail "reinstating returned $status, want 200"; }
[[ "$(json "$WORKDIR/stand-back.json" '["from"]')" == "suspended" ]] \
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
