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
