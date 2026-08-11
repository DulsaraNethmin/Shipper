# shellcheck shell=bash
#
# M2 jobs — the address value object, the draft endpoints, and the rules a client cannot see.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 50–59 is the jobs range. This file is the jobs track's to append to, and no other track's to
# edit — which is the whole reason the script was split (Docs/11 §9, SHIP-15e).
#
# The sections here are ordered rather than independent: the accounts and the token are made once
# at the top and read by everything after. They are made through the API rather than reused from
# 40-identity.sh, on the runner's own advice — a section that needs its own account registers one.
#
# # Why the tokens are minted rather than obtained
#
# There is no sign-in endpoint yet (SHIP-41). mint_token is in the runner, signed with the
# development key from deploy/.env.example — public, in this repository on purpose, and refused by
# the service outside development. It carries `role: customer` for every subject, which is exactly
# the thing worth exercising here: the platform decides what a caller may do by reading the
# account, not by believing the claim, so a provider's token minted with a customer claim must
# still be refused.

# --- the accounts these checks run as ----------------------------------------------------------

jobs_customer_email="job-customer-$$@example.com"
status="$(post_json "verify-jobs-cust-$$" /v1/auth/register \
  "{\"email\":\"$jobs_customer_email\",\"phone\":\"04130$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/jobs-customer.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-customer.json"; fail "could not register the job customer: $status"; }
jobs_customer_id="$(json "$WORKDIR/jobs-customer.json" '["id"]')"
jobs_customer_token="$(mint_token "$jobs_customer_id")"

status="$(post_json "verify-jobs-other-$$" /v1/auth/register \
  "{\"email\":\"job-other-$$@example.com\",\"phone\":\"04131$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/jobs-other.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-other.json"; fail "could not register the second customer: $status"; }
jobs_other_token="$(mint_token "$(json "$WORKDIR/jobs-other.json" '["id"]')")"

status="$(post_json "verify-jobs-prov-$$" /v1/auth/register \
  "{\"email\":\"job-provider-$$@example.com\",\"phone\":\"04132$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/jobs-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-provider.json"; fail "could not register the provider: $status"; }
jobs_provider_token="$(mint_token "$(json "$WORKDIR/jobs-provider.json" '["id"]')")"

# job_request <method> <token> <key> <path> <body> <outfile> — one authenticated state-changing
# request, answering with its status.
#
# Both endpoints are protected and state-changing, so every call needs a credential *and* an
# idempotency key. Passing both as arguments means a check that forgets one gets a compile-time
# sort of failure — a wrong number of arguments — rather than a 400 or a 401 that has nothing to do
# with what it was testing.
job_request() {
  curl -s -X "$1" -o "$6" -w '%{http_code}' \
    -H "$auth_header: Bearer $2" \
    -H "Idempotency-Key: $3" \
    -H 'Content-Type: application/json' \
    -d "$5" "http://localhost:$VERIFY_PORT$4"
}

# ---------------------------------------------------------------------------------------
ticket "SHIP-61  POST /v1/jobs creates a Draft owned by the calling customer"

status="$(curl -s -X POST -o "$WORKDIR/jobs-anon.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-jobs-anon-$$" -H 'Content-Type: application/json' \
  -d '{}' "http://localhost:$VERIFY_PORT/v1/jobs")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/jobs-anon.json"; fail "an unauthenticated POST /v1/jobs returned $status, want 401"; }
ok "it cannot be reached without a credential"

status="$(job_request POST "$jobs_provider_token" "verify-jobs-prov-post-$$" /v1/jobs '{}' \
  "$WORKDIR/jobs-provider-post.json")"
[[ "$status" == "403" ]] || { cat "$WORKDIR/jobs-provider-post.json"; fail "a provider created a job: $status"; }
[[ "$(json "$WORKDIR/jobs-provider-post.json" '["error"]["code"]')" == "jobs_customer_only" ]] \
  || { cat "$WORKDIR/jobs-provider-post.json"; fail "expected code=jobs_customer_only"; }
ok "a provider is refused, with a code the app can act on — and the token claimed 'customer'"

# The empty body is not laziness: Docs/01 §4.1 saves drafts and the app captures a job over
# several steps, so "start a job for me" has to work before anything has been filled in.
status="$(job_request POST "$jobs_customer_token" "verify-jobs-empty-$$" /v1/jobs '{}' \
  "$WORKDIR/jobs-empty.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-empty.json"; fail "POST /v1/jobs returned $status, want 201"; }
[[ "$(json "$WORKDIR/jobs-empty.json" '["status"]')" == "draft" ]] \
  || fail "a new job is not a draft: $(json "$WORKDIR/jobs-empty.json" '["status"]')"
ok "an empty body creates an empty draft, which is what a job wizard's first step needs"

job_body='{
  "pickup":  {"line": "  12   Smith Street ", "suburb": "Newtown", "state": "nsw", "postcode": "2042"},
  "dropoff": {"line": "40 Bourke Street", "suburb": "Melbourne", "state": "Victoria", "postcode": "3000"},
  "goods_description": "Two-seater sofa, wrapped",
  "weight_kg": 45.5,
  "handling_notes": "Second-floor walk-up, no lift.",
  "pickup_window": {"start": "2026-08-14T09:00:00+10:00", "end": "2026-08-15T17:00:00+10:00"}
}'

status="$(job_request POST "$jobs_customer_token" "verify-jobs-create-$$" /v1/jobs "$job_body" \
  "$WORKDIR/job.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/job.json"; fail "POST /v1/jobs returned $status, want 201"; }
job_id="$(json "$WORKDIR/job.json" '["id"]')"
[[ "$job_id" =~ ^[0-9a-f-]{36}$ ]] || fail "the response carries no job id: $job_id"
ok "a filled-in draft is created and answered with 201"

# The response is one thing; the row is another. Ownership and status are read from the columns
# rather than from the answer the endpoint gave about itself.
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select customer_id || ' ' || status from jobs where id = '$job_id';")"
[[ "$stored" == "$jobs_customer_id Draft" ]] \
  || fail "the stored job is '$stored', want '$jobs_customer_id Draft'"
ok "the row is owned by the calling customer and its status is Draft"

# ---------------------------------------------------------------------------------------
ticket "SHIP-60  pickup and drop-off validate and normalise, with coordinates resolved"

[[ "$(json "$WORKDIR/job.json" '["pickup"]["line"]')" == "12 Smith Street" ]] \
  || fail "the pickup line was not normalised: $(json "$WORKDIR/job.json" '["pickup"]["line"]')"
[[ "$(json "$WORKDIR/job.json" '["pickup"]["state"]')" == "NSW" ]] \
  || fail "'nsw' was not normalised to NSW"
[[ "$(json "$WORKDIR/job.json" '["dropoff"]["state"]')" == "VIC" ]] \
  || fail "'Victoria' was not normalised to VIC"
ok "whitespace is collapsed and any form of a state becomes its abbreviation"

pickup_lat="$(json "$WORKDIR/job.json" '["pickup"]["coordinate"]["latitude"]')"
pickup_lng="$(json "$WORKDIR/job.json" '["pickup"]["coordinate"]["longitude"]')"
python3 -c "import sys; lat, lng = float(sys.argv[1]), float(sys.argv[2]); sys.exit(0 if -43.6 <= lat <= -10.7 and 113.3 <= lng <= 153.6 else 1)" \
  "$pickup_lat" "$pickup_lng" \
  || fail "the pickup resolved to ($pickup_lat, $pickup_lng), which is not in Australia"
ok "both addresses resolved to a coordinate ($pickup_lat, $pickup_lng)"

# The coordinate is on the row, not merely in the response — and it is a pair, which
# ck_jobs_pickup_coordinate_is_a_pair is what makes structural.
stored_coordinate="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (pickup_latitude is not null) and (pickup_longitude is not null)
     and (dropoff_latitude is not null) and (dropoff_longitude is not null)
   from jobs where id = '$job_id';")"
[[ "$stored_coordinate" == "t" ]] || fail "the resolved coordinates were not stored"
ok "the coordinates are on the row, both pairs complete"

status="$(job_request POST "$jobs_customer_token" "verify-jobs-badaddr-$$" /v1/jobs \
  '{"pickup": {"line": "1 High Street", "suburb": "Somewhere", "state": "Westeros", "postcode": "20"}}' \
  "$WORKDIR/jobs-badaddr.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/jobs-badaddr.json"; fail "a malformed address returned $status, want 422"; }
[[ "$(json "$WORKDIR/jobs-badaddr.json" '["error"]["code"]')" == "validation_failed" ]] \
  || { cat "$WORKDIR/jobs-badaddr.json"; fail "expected code=validation_failed"; }
grep -q '"pickup.state"' "$WORKDIR/jobs-badaddr.json" || fail "no detail names pickup.state"
grep -q '"pickup.postcode"' "$WORKDIR/jobs-badaddr.json" || fail "no detail names pickup.postcode"
ok "an address that is not Australian is refused, naming every offending field at once"

# An address given in pieces is refused rather than half-stored. Docs/01 §4.1 allows an *absent*
# address and this is the other case: started, and stopped halfway.
status="$(job_request POST "$jobs_customer_token" "verify-jobs-partial-$$" /v1/jobs \
  '{"pickup": {"suburb": "Newtown"}}' "$WORKDIR/jobs-partial.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/jobs-partial.json"; fail "a partial address returned $status, want 422"; }
ok "a partly filled address is refused, while an absent one is not"

# ---------------------------------------------------------------------------------------
ticket "SHIP-62  PATCH /v1/jobs/{id} updates a Draft and rejects edits by non-owners"

status="$(job_request PATCH "$jobs_customer_token" "verify-jobs-edit-$$" "/v1/jobs/$job_id" \
  '{"goods_description": "Three-seater sofa, wrapped", "handling_notes": "", "length_cm": 190}' \
  "$WORKDIR/jobs-edited.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-edited.json"; fail "PATCH returned $status, want 200"; }
[[ "$(json "$WORKDIR/jobs-edited.json" '["goods_description"]')" == "Three-seater sofa, wrapped" ]] \
  || fail "the description was not updated"
[[ "$(json "$WORKDIR/jobs-edited.json" '["length_cm"]')" == "190" ]] || fail "the length was not set"
ok "the owner's edit is applied"

# A field that was not mentioned is left alone, and one sent empty is cleared. Those are two
# different things and a client that could not express the second could never remove a note.
[[ "$(json "$WORKDIR/jobs-edited.json" '["weight_kg"]')" == "45.5" ]] \
  || fail "a field that was not mentioned changed: weight_kg"
[[ "$(json "$WORKDIR/jobs-edited.json" '["pickup"]["line"]')" == "12 Smith Street" ]] \
  || fail "an address that was not mentioned changed"
grep -q '"handling_notes"' "$WORKDIR/jobs-edited.json" && fail "the cleared note is still in the response"
ok "absent means unchanged and empty means cleared, which are not the same request"

# The address that was not mentioned kept the coordinate it already had.
[[ "$(json "$WORKDIR/jobs-edited.json" '["pickup"]["coordinate"]["latitude"]')" == "$pickup_lat" ]] \
  || fail "an untouched address lost or changed its coordinate"
ok "an untouched address keeps its resolved coordinate"

status="$(job_request PATCH "$jobs_other_token" "verify-jobs-stranger-$$" "/v1/jobs/$job_id" \
  '{"goods_description": "A cheap sofa"}' "$WORKDIR/jobs-stranger.json")"
[[ "$status" == "404" ]] \
  || { cat "$WORKDIR/jobs-stranger.json"; fail "a stranger's edit returned $status, want 404"; }
ok "another customer's edit is refused"

# 404 rather than 403, and byte-identical to a job that does not exist. A 403 would confirm that
# a job with this id has been created, which is something a stranger has no way to learn.
status="$(job_request PATCH "$jobs_other_token" "verify-jobs-missing-$$" \
  "/v1/jobs/00000000-0000-7000-8000-000000000000" \
  '{"goods_description": "A cheap sofa"}' "$WORKDIR/jobs-missing.json")"
[[ "$status" == "404" ]] || fail "a job that does not exist returned $status, want 404"
python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$WORKDIR/jobs-stranger.json" "$WORKDIR/jobs-missing.json" \
  || fail "somebody else's job answers differently from no job at all, which discloses that it exists"
ok "and is indistinguishable from a job that was never created"

# The refusal is a refusal, not a rollback that happened to work.
still="$("$PSQL" "$DATABASE_URL" -tAc \
  "select goods_description from jobs where id = '$job_id';")"
[[ "$still" == "Three-seater sofa, wrapped" ]] || fail "the refused edit changed the row: $still"
ok "nothing the stranger sent reached the row"

# ---------------------------------------------------------------------------------------
ticket "SHIP-57  job status is not a settable field, through the API either"

for verb_and_target in "POST /v1/jobs" "PATCH /v1/jobs/$job_id"; do
  verb="${verb_and_target%% *}"
  target="${verb_and_target##* }"

  status="$(job_request "$verb" "$jobs_customer_token" "verify-jobs-setstatus-$verb-$$" \
    "$target" '{"status": "open"}' "$WORKDIR/jobs-setstatus.json")"
  [[ "$status" == "400" ]] \
    || { cat "$WORKDIR/jobs-setstatus.json"; fail "$verb accepted a status field: $status"; }
  grep -q 'status' "$WORKDIR/jobs-setstatus.json" || fail "the refusal does not name the field"
done
ok "neither endpoint accepts a status field; the unknown field is reported, not ignored"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$job_id';")" == "Draft" ]] \
  || fail "the job moved"
ok "the job is still a Draft"

# ---------------------------------------------------------------------------------------
ticket "SHIP-64  POST /v1/jobs/{id}/cancel ends a Draft or an unawarded Open job"

# new_draft <name> — a fresh draft owned by the job customer, answering with its id.
#
# One per check below, because a cancellation is terminal: reusing $job_id would make every
# check after the first one depend on the order they happen to run in.
new_draft() {
  local out="$WORKDIR/jobs-$1.json"
  [[ "$(job_request POST "$jobs_customer_token" "verify-jobs-$1-$$" /v1/jobs '{}' "$out")" == "201" ]] \
    || { cat "$out"; fail "could not create a draft for $1"; }
  json "$out" '["id"]'
}

# move_job <job-id> <from> <to> — put a job into a state no endpoint can reach yet.
#
# SHIP-63 publishes and SHIP-92 awards, and neither exists. The only way to reach Open or
# Awarded is therefore the protocol 000402's trigger demands: a job_status_history row written
# in the same transaction, naming this job and this exact move, pointed at by a
# transaction-local setting. A bare `UPDATE jobs SET status` is refused — which is the point.
# This fixture cannot bypass the guard even deliberately, so a check that runs after it is
# looking at a job that got where it is honestly.
#
# The SQL is single-quoted on purpose: `$$` in a double-quoted shell string is the process id,
# which is how a dollar-quoted PL/pgSQL block would silently become nonsense. Values come in as
# psql variables instead.
move_job() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 \
    -v job="$1" -v from_status="$2" -v to_status="$3" -v actor="$jobs_customer_id" \
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

draft_to_cancel="$(new_draft cancel-draft)"
status="$(job_request POST "$jobs_customer_token" "verify-jobs-cancel-draft-do-$$" \
  "/v1/jobs/$draft_to_cancel/cancel" '{"reason": "Found a cheaper option elsewhere."}' \
  "$WORKDIR/jobs-cancelled.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-cancelled.json"; fail "cancelling a draft returned $status, want 200"; }
[[ "$(json "$WORKDIR/jobs-cancelled.json" '["status"]')" == "cancelled" ]] \
  || fail "the response says $(json "$WORKDIR/jobs-cancelled.json" '["status"]')"
ok "a customer cancels their own draft"

# The row, not the answer the endpoint gave about itself — and the history row beside it,
# because 000402 refuses the status write without one. A job that reached Cancelled with no
# recorded transition would mean the guard had been bypassed.
cancelled_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status || ' ' || h.from_status || '->' || h.to_status || ' ' || h.actor_type
     from jobs j join job_status_history h on h.job_id = j.id
    where j.id = '$draft_to_cancel';")"
[[ "$cancelled_row" == "Cancelled Draft->Cancelled customer" ]] \
  || fail "the stored transition is '$cancelled_row', want 'Cancelled Draft->Cancelled customer'"
ok "the move went through the guard and left the record 000402 requires"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select reason from job_status_history where job_id = '$draft_to_cancel';")" \
  == "Found a cheaper option elsewhere." ]] || fail "the customer's reason was not recorded"
ok "the reason is recorded against the transition, for support to read"

# Published, then cancelled. This is the half of SHIP-64's Done when that a Draft-only
# implementation would pass without.
open_to_cancel="$(new_draft cancel-open)"
move_job "$open_to_cancel" Draft Open || fail "could not publish a job for the Open case"
status="$(job_request POST "$jobs_customer_token" "verify-jobs-cancel-open-do-$$" \
  "/v1/jobs/$open_to_cancel/cancel" '{}' "$WORKDIR/jobs-cancel-open.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-cancel-open.json"; fail "cancelling an Open job returned $status, want 200"; }
[[ "$(json "$WORKDIR/jobs-cancel-open.json" '["status"]')" == "cancelled" ]] \
  || fail "the Open job did not reach cancelled"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select from_status from job_status_history where job_id = '$open_to_cancel' and to_status = 'Cancelled';")" \
  == "Open" ]] || fail "the recorded move is not Open->Cancelled"
ok "an unawarded Open job is cancelled too, and a body with no reason is a complete request"

# An awarded job is refused, and the refusal comes from Docs/02 §2's table rather than from a
# list inside the endpoint.
awarded_job="$(new_draft cancel-awarded)"
move_job "$awarded_job" Draft Open   || fail "could not publish the job to be awarded"
move_job "$awarded_job" Open Awarded || fail "could not move a job to Awarded"

status="$(job_request POST "$jobs_customer_token" "verify-jobs-cancel-awarded-do-$$" \
  "/v1/jobs/$awarded_job/cancel" '{}' "$WORKDIR/jobs-cancel-awarded.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/jobs-cancel-awarded.json"; fail "cancelling an awarded job returned $status, want 409"; }
[[ "$(json "$WORKDIR/jobs-cancel-awarded.json" '["error"]["code"]')" == "jobs_not_cancellable" ]] \
  || { cat "$WORKDIR/jobs-cancel-awarded.json"; fail "expected code=jobs_not_cancellable"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$awarded_job';")" == "Awarded" ]] \
  || fail "the refused cancellation moved the job"
ok "an awarded job cannot be cancelled — Docs/02 §6.2 makes that a support matter"

# Cancelling again, with a different key so the idempotency middleware is not what absorbs it.
status="$(job_request POST "$jobs_customer_token" "verify-jobs-cancel-again-$$" \
  "/v1/jobs/$draft_to_cancel/cancel" '{}' "$WORKDIR/jobs-cancel-again.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-cancel-again.json"; fail "cancelling twice returned $status, want 200"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$draft_to_cancel';")" == "1" ]] \
  || fail "cancelling twice recorded the transition twice"
ok "a second cancellation with a fresh key is absorbed, and records nothing further"

# A stranger's cancellation is a 404, byte-identical to a job that does not exist. The rule is
# per endpoint rather than per domain: a job is discoverable through whichever route forgets it.
victim="$(new_draft cancel-victim)"
status="$(job_request POST "$jobs_other_token" "verify-jobs-cancel-stranger-$$" \
  "/v1/jobs/$victim/cancel" '{}' "$WORKDIR/jobs-cancel-stranger.json")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/jobs-cancel-stranger.json"; fail "a stranger's cancellation returned $status, want 404"; }
status="$(job_request POST "$jobs_other_token" "verify-jobs-cancel-nothing-$$" \
  "/v1/jobs/00000000-0000-7000-8000-000000000001/cancel" '{}' "$WORKDIR/jobs-cancel-nothing.json")"
[[ "$status" == "404" ]] || fail "cancelling a job that does not exist returned $status, want 404"
python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$WORKDIR/jobs-cancel-stranger.json" "$WORKDIR/jobs-cancel-nothing.json" \
  || fail "somebody else's job answers differently from no job at all"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$victim';")" == "Draft" ]] \
  || fail "the stranger's refused cancellation reached the row"
ok "a stranger cannot cancel, and cannot tell the job apart from one that never existed"

# ---------------------------------------------------------------------------------------
ticket "SHIP-65  GET /v1/jobs/{id} returns the full job to the owning customer only"

# job_get <token> <path> <outfile> — one authenticated read.
#
# No Idempotency-Key, and that is the point of a separate helper rather than a sixth argument to
# job_request: a GET changes nothing, the middleware lets read-only methods through untouched,
# and a check that sent a key anyway would be demonstrating something other than what it claims.
job_get() {
  curl -s -o "$3" -w '%{http_code}' -H "$auth_header: Bearer $1" \
    "http://localhost:$VERIFY_PORT$2"
}

status="$(curl -s -o "$WORKDIR/jobs-detail-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$job_id")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/jobs-detail-anon.json"; fail "an unauthenticated read returned $status, want 401"; }
ok "it cannot be reached without a credential"

status="$(job_get "$jobs_customer_token" "/v1/jobs/$job_id" "$WORKDIR/jobs-detail.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-detail.json"; fail "GET /v1/jobs/{id} returned $status, want 200"; }
ok "the owner reads their own job"

# Every field the customer supplied comes back, including the ones SHIP-62 changed. "In full"
# is checked against the columns rather than against a list somebody remembered to write.
python3 - "$WORKDIR/jobs-detail.json" <<'PY' || fail "the detail response is not the whole job"
import json, sys
job = json.load(open(sys.argv[1]))
expected = {
    "status": "draft",
    "goods_description": "Three-seater sofa, wrapped",
    "weight_kg": 45.5,
    "length_cm": 190,
}
missing = {k: v for k, v in expected.items() if job.get(k) != v}
if missing:
    print("wrong or absent:", missing, file=sys.stderr)
    sys.exit(1)
if job["pickup"]["state"] != "NSW" or job["dropoff"]["state"] != "VIC":
    print("an address did not come back:", job.get("pickup"), job.get("dropoff"), file=sys.stderr)
    sys.exit(1)
if "coordinate" not in job["pickup"]:
    print("the resolved coordinate did not come back", file=sys.stderr)
    sys.exit(1)
PY
ok "it carries everything the customer supplied, addresses and coordinates included"

# One shape, whatever the client did to obtain the job. A "detail" response with a field or two
# more would make every write response a subset a client has to special-case.
python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))
b = json.load(open(sys.argv[2]))
sys.exit(0 if a == b else 1)
" "$WORKDIR/jobs-detail.json" "$WORKDIR/jobs-edited.json" \
  || fail "the read and the write answer with different shapes"
ok "the same shape the write endpoints answer with, so a client parses one type"

# There is no budget field, and there will not be one until SHIP-67 brings the test proving it
# cannot reach a provider. This check is here so that the day it does arrive, somebody has to
# come to this file and say so deliberately.
grep -q 'budget' "$WORKDIR/jobs-detail.json" && fail "a budget field appeared before SHIP-67"
ok "no budget field yet — SHIP-67 lands the column together with its serialisation proof"

status="$(job_get "$jobs_other_token" "/v1/jobs/$job_id" "$WORKDIR/jobs-detail-stranger.json")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/jobs-detail-stranger.json"; fail "a stranger's read returned $status, want 404"; }
status="$(job_get "$jobs_other_token" "/v1/jobs/00000000-0000-7000-8000-000000000002" \
  "$WORKDIR/jobs-detail-nothing.json")"
[[ "$status" == "404" ]] || fail "reading a job that does not exist returned $status, want 404"
python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$WORKDIR/jobs-detail-stranger.json" "$WORKDIR/jobs-detail-nothing.json" \
  || fail "somebody else's job answers differently from no job at all"
grep -q 'sofa' "$WORKDIR/jobs-detail-stranger.json" && fail "the refusal leaked the job's contents"
ok "a stranger gets the same 404 a missing job gets, and learns nothing from it"

# ---------------------------------------------------------------------------------------
ticket "SHIP-66  GET /v1/jobs lists the customer's own jobs, filtered and paginated"

# The second customer gets a job of their own, so "only the caller's" is a claim with something
# to be wrong about rather than a list that happens to be short.
status="$(job_request POST "$jobs_other_token" "verify-jobs-list-other-$$" /v1/jobs \
  '{"goods_description": "belongs to somebody else"}' "$WORKDIR/jobs-list-other.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-list-other.json"; fail "could not create the other customer's job"; }

status="$(job_get "$jobs_customer_token" /v1/jobs "$WORKDIR/jobs-list.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-list.json"; fail "GET /v1/jobs returned $status, want 200"; }
grep -q 'belongs to somebody else' "$WORKDIR/jobs-list.json" \
  && fail "the list carries another customer's job"
ok "a customer sees their own jobs and nobody else's"

# Newest first, and the envelope is Docs/10 §4.5's — data, next_cursor, has_more.
python3 - "$WORKDIR/jobs-list.json" <<'PY' || fail "the list is not the envelope Docs/10 §4.5 describes"
import json, sys
page = json.load(open(sys.argv[1]))
if set(page) - {"data", "next_cursor", "has_more"}:
    print("unexpected keys:", set(page), file=sys.stderr); sys.exit(1)
if not isinstance(page["data"], list) or "has_more" not in page:
    print("wrong shape:", page, file=sys.stderr); sys.exit(1)
created = [job["created_at"] for job in page["data"]]
if created != sorted(created, reverse=True):
    print("not newest first:", created, file=sys.stderr); sys.exit(1)
PY
ok "the envelope is data/next_cursor/has_more, newest first"

# The filter takes the wire form of the status, which is the form every other response uses.
status="$(job_get "$jobs_customer_token" '/v1/jobs?status=cancelled' "$WORKDIR/jobs-list-cancelled.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-list-cancelled.json"; fail "filtering returned $status"; }
python3 - "$WORKDIR/jobs-list-cancelled.json" <<'PY' || fail "the status filter did not filter"
import json, sys
page = json.load(open(sys.argv[1]))
wrong = [job["status"] for job in page["data"] if job["status"] != "cancelled"]
if wrong or not page["data"]:
    print("statuses:", wrong or "none at all", file=sys.stderr); sys.exit(1)
PY
ok "?status= narrows the list, and takes the wire form every response uses"

# The stored form is not the wire form and neither is a status that does not exist. Both are
# refused rather than answered with an empty list, which would tell a client its filter worked.
status="$(job_get "$jobs_customer_token" '/v1/jobs?status=Draft' "$WORKDIR/jobs-list-badstatus.json")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/jobs-list-badstatus.json"; fail "?status=Draft returned $status, want 400"; }
status="$(job_get "$jobs_customer_token" '/v1/jobs?status=nonsense' "$WORKDIR/jobs-list-badstatus.json")"
[[ "$status" == "400" ]] || fail "?status=nonsense returned $status, want 400"
ok "a status that is not one of the twelve is refused, not answered with an empty list"

# Paging, followed the way a client follows it: take next_cursor, send it back, stop at
# has_more=false. The page size is 1 so the boundary is crossed several times.
python3 - "$jobs_customer_token" "$VERIFY_PORT" "$auth_header" <<'PY' || fail "paging did not reach every job exactly once"
import json, sys, urllib.parse, urllib.request

token, port, header = sys.argv[1], sys.argv[2], sys.argv[3]
base = f"http://localhost:{port}/v1/jobs"

def get(url):
    request = urllib.request.Request(url, headers={header: f"Bearer {token}"})
    with urllib.request.urlopen(request) as response:
        return json.load(response)

whole = get(f"{base}?limit=100")
if whole["has_more"]:
    print("the fixture has more than 100 jobs; this check assumes it does not", file=sys.stderr)
    sys.exit(1)
expected = {job["id"] for job in whole["data"]}

seen, url, pages = [], f"{base}?limit=1", 0
while True:
    pages += 1
    if pages > len(expected) + 2:
        print("paging did not terminate; the cursor is not advancing", file=sys.stderr)
        sys.exit(1)
    page = get(url)
    seen.extend(job["id"] for job in page["data"])
    if not page["has_more"]:
        if page.get("next_cursor"):
            print("the last page carries a cursor", file=sys.stderr)
            sys.exit(1)
        break
    if len(page["data"]) != 1:
        print("a page before the last holds", len(page["data"]), "jobs, want the limit of 1", file=sys.stderr)
        sys.exit(1)
    url = f"{base}?limit=1&cursor={urllib.parse.quote(page['next_cursor'])}"

if sorted(seen) != sorted(expected) or len(seen) != len(set(seen)):
    print("paged over", sorted(seen), "want", sorted(expected), file=sys.stderr)
    sys.exit(1)
print(f"    {len(expected)} jobs over {pages} pages of one")
PY
ok "paging one job at a time reaches every job exactly once, and terminates"

# A cursor this endpoint did not issue is refused rather than read as "start again", which would
# quietly restart a client's paging loop from the top.
status="$(job_get "$jobs_customer_token" '/v1/jobs?cursor=not-a-cursor' "$WORKDIR/jobs-list-badcursor.json")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/jobs-list-badcursor.json"; fail "a mangled cursor returned $status, want 400"; }
status="$(job_get "$jobs_customer_token" '/v1/jobs?limit=0' "$WORKDIR/jobs-list-badlimit.json")"
[[ "$status" == "400" ]] || fail "?limit=0 returned $status, want 400"
status="$(job_get "$jobs_customer_token" '/v1/jobs?limit=5000' "$WORKDIR/jobs-list-biglimit.json")"
[[ "$status" == "200" ]] || fail "?limit=5000 returned $status, want it narrowed to the maximum"
ok "a mangled cursor and a nonsense limit are refused; an over-large limit is narrowed"

# A customer with no jobs gets an empty array rather than null. A client iterating null breaks the
# first time a new customer opens the app, and never again in testing.
status="$(post_json "verify-jobs-fresh-$$" /v1/auth/register \
  "{\"email\":\"job-fresh-$$@example.com\",\"phone\":\"04133$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/jobs-fresh.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-fresh.json"; fail "could not register a customer with no jobs"; }
status="$(job_get "$(mint_token "$(json "$WORKDIR/jobs-fresh.json" '["id"]')")" /v1/jobs \
  "$WORKDIR/jobs-list-empty.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-list-empty.json"; fail "an empty list returned $status"; }
[[ "$(tr -d ' \n' < "$WORKDIR/jobs-list-empty.json")" == '{"data":[],"has_more":false}' ]] \
  || { cat "$WORKDIR/jobs-list-empty.json"; fail "an empty list is not an empty array"; }
ok "a customer with no jobs gets an empty array, never null"
