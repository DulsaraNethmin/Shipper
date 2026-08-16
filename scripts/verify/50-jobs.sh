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
  "{\"name\":\"Verify Harness\",\"email\":\"$jobs_customer_email\",\"phone\":\"04130$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/jobs-customer.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-customer.json"; fail "could not register the job customer: $status"; }
jobs_customer_id="$(json "$WORKDIR/jobs-customer.json" '["id"]')"
jobs_customer_token="$(mint_token "$jobs_customer_id")"

status="$(post_json "verify-jobs-other-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"job-other-$$@example.com\",\"phone\":\"04131$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/jobs-other.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-other.json"; fail "could not register the second customer: $status"; }
jobs_other_token="$(mint_token "$(json "$WORKDIR/jobs-other.json" '["id"]')")"

status="$(post_json "verify-jobs-prov-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"job-provider-$$@example.com\",\"phone\":\"04132$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
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

# # The budget tripwire, moved deliberately at SHIP-67
#
# This check used to read `grep -q budget && fail "a budget field appeared before SHIP-67"`, and
# it was there so that the day the column arrived, somebody had to come to this file and say so.
# This is that day, and this is that sentence.
#
# What replaces it is stronger rather than weaker, which is the only acceptable direction for
# this particular assertion (Docs/11 §8). Here it stays narrow: this job was created without a
# budget, so the field must be *absent* — the omitempty rule that lets a client tell "no budget"
# from "a budget of nothing". The real proof, that the owner sees theirs and nobody else sees it
# in any form, is the SHIP-67 section below.
grep -q 'budget' "$WORKDIR/jobs-detail.json" \
  && fail "a job created with no budget came back carrying one"
ok "a job with no budget omits the field rather than sending zero"

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
  "{\"name\":\"Verify Harness\",\"email\":\"job-fresh-$$@example.com\",\"phone\":\"04133$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/jobs-fresh.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-fresh.json"; fail "could not register a customer with no jobs"; }
status="$(job_get "$(mint_token "$(json "$WORKDIR/jobs-fresh.json" '["id"]')")" /v1/jobs \
  "$WORKDIR/jobs-list-empty.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-list-empty.json"; fail "an empty list returned $status"; }
[[ "$(tr -d ' \n' < "$WORKDIR/jobs-list-empty.json")" == '{"data":[],"has_more":false}' ]] \
  || { cat "$WORKDIR/jobs-list-empty.json"; fail "an empty list is not an empty array"; }
ok "a customer with no jobs gets an empty array, never null"

# ---------------------------------------------------------------------------------------
ticket "SHIP-67  the budget is stored for its owner and never serialised to a provider"

# Docs/01 §4.3: the customer's maximum is private from providers — not as an amount, not as a
# band, and not as a "budget supplied" indicator. CLAUDE.md lists it among the invariants whose
# violation is a defect rather than a style choice.
#
# The proof here is deliberately negative and exhaustive rather than a single check on a single
# endpoint: every response a provider account can obtain from this domain is searched for the
# word, in any form, and so is the domain event — which travels further than any endpoint, to
# Kafka and whatever is behind it (SHIP-134).

status="$(job_request POST "$jobs_customer_token" "verify-jobs-budget-$$" /v1/jobs \
  '{"goods_description": "Pallet of floor tiles", "budget_cents": 150000}' \
  "$WORKDIR/jobs-budget.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-budget.json"; fail "creating a job with a budget returned $status, want 201"; }
budget_job="$(json "$WORKDIR/jobs-budget.json" '["id"]')"
[[ "$(json "$WORKDIR/jobs-budget.json" '["budget_cents"]')" == "150000" ]] \
  || { cat "$WORKDIR/jobs-budget.json"; fail "the create response did not carry the budget back"; }
ok "a customer sets a maximum on their own job, in cents"

# The column, not the answer the endpoint gave about itself. numeric(12,2) and never a float
# (Docs/10 §3.3), so the cents survive the round trip exactly.
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select budget from jobs where id = '$budget_job';")" == "1500.00" ]] \
  || fail "the stored budget is $("$PSQL" "$DATABASE_URL" -tAc "select budget from jobs where id = '$budget_job';"), want 1500.00"
ok "it is stored as numeric(12,2) — 150000 cents is \$1500.00, exactly"

status="$(job_get "$jobs_customer_token" "/v1/jobs/$budget_job" "$WORKDIR/jobs-budget-detail.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-budget-detail.json"; fail "reading the job returned $status"; }
[[ "$(json "$WORKDIR/jobs-budget-detail.json" '["budget_cents"]')" == "150000" ]] \
  || { cat "$WORKDIR/jobs-budget-detail.json"; fail "the owner cannot read their own budget back"; }
ok "the owning customer reads it back — SHIP-65's 'full job including budget', now in full"

# And nobody else does, through any route this domain serves. A provider holding a valid token is
# exactly the caller Docs/01 §4.3 is about; a second customer is the other half of the same rule.
budget_leaks=""
for who_and_token in "a provider:$jobs_provider_token" "another customer:$jobs_other_token"; do
  who="${who_and_token%%:*}"
  token="${who_and_token#*:}"

  job_get "$token" "/v1/jobs/$budget_job" "$WORKDIR/jobs-budget-read.json"    >/dev/null
  job_get "$token" /v1/jobs               "$WORKDIR/jobs-budget-list.json"    >/dev/null
  job_request PATCH "$token" "verify-jobs-budget-patch-$who-$$" "/v1/jobs/$budget_job" \
    '{"budget_cents": 1}' "$WORKDIR/jobs-budget-patch.json" >/dev/null
  job_request POST "$token" "verify-jobs-budget-cancel-$who-$$" "/v1/jobs/$budget_job/cancel" \
    '{}' "$WORKDIR/jobs-budget-refuse.json" >/dev/null

  for answer in read list patch refuse; do
    grep -qi 'budget' "$WORKDIR/jobs-budget-$answer.json" \
      && budget_leaks="$budget_leaks $who/$answer"
    grep -q '150000' "$WORKDIR/jobs-budget-$answer.json" \
      && budget_leaks="$budget_leaks $who/$answer(amount)"
  done
done
[[ -z "$budget_leaks" ]] || fail "the budget reached somebody who is not the owner:$budget_leaks"
ok "no read, list, edit or cancellation by a provider or another customer mentions it at all"

# The validator answers in the error contract's shape rather than letting ck_jobs_budget answer,
# because a client told 'ck_jobs_budget' can do nothing with that (Docs/10 §4.6).
status="$(job_request POST "$jobs_customer_token" "verify-jobs-budget-bad-$$" /v1/jobs \
  '{"budget_cents": -1}' "$WORKDIR/jobs-budget-bad.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/jobs-budget-bad.json"; fail "a negative budget returned $status, want 422"; }
grep -q '"budget_cents"' "$WORKDIR/jobs-budget-bad.json" || fail "the refusal does not name budget_cents"
ok "a budget that is not an amount is refused, naming the field"

# The domain event is the copy of a job that travels furthest — past the last endpoint that could
# have redacted anything. Cancelling is the transition available over HTTP today; the expiry event
# below is checked the same way.
status="$(job_request POST "$jobs_customer_token" "verify-jobs-budget-end-$$" \
  "/v1/jobs/$budget_job/cancel" '{}' "$WORKDIR/jobs-budget-end.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-budget-end.json"; fail "cancelling the job returned $status"; }
budget_event="$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(string_agg(payload::text, ' '), '') from outbox where aggregate_id = '$budget_job';")"
[[ -n "$budget_event" ]] || fail "the transition emitted no event, so this check proves nothing"
case "$budget_event" in
  *budget*|*150000*) fail "a domain event carries the customer's budget: $budget_event" ;;
esac
ok "the domain event carries no budget, in a payload that reaches every consumer there will be"

# ---------------------------------------------------------------------------------------
ticket "SHIP-68  an Open job expires at the earlier of fourteen days or its pickup date"

# Docs/02 §6.3, demonstrated against the running database and the real worker binary rather than
# asserted. Three parts have to hold: publication sets a deadline, the deadline is the earlier of
# the two rules, and a process acts on it — through the SHIP-57 guard, as the platform.
#
# The deadline is set by 000406's trigger on the transition itself, so it appears however the job
# reaches Open. move_job above publishes with raw SQL through the guard's own protocol, which is
# what makes that worth showing here: a route into Open that nobody has written yet still gets a
# deadline.

# Dates computed with python3 rather than date(1). BSD date takes -v and GNU date takes -d, and
# this script runs on a developer's macOS and on CI's Linux.
past_pickup="$(python3 -c 'import datetime as d; print((d.datetime.now(d.timezone.utc) - d.timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"

status="$(job_request POST "$jobs_customer_token" "verify-jobs-stale-$$" /v1/jobs \
  "{\"goods_description\": \"Two pallets, urgent\", \"pickup_window\": {\"end\": \"$past_pickup\"}}" \
  "$WORKDIR/jobs-stale.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-stale.json"; fail "creating the stale job returned $status"; }
stale_job="$(json "$WORKDIR/jobs-stale.json" '["id"]')"

live_job="$(new_draft expiry-live)"
draft_job="$(new_draft expiry-draft)"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select expires_at is null from jobs where id = '$draft_job';")" == "t" ]] \
  || fail "a draft already has a deadline; the clock starts at publication (Docs/02 §6.3)"
ok "a draft carries no deadline — a saved draft may sit indefinitely (Docs/01 §4.1)"

move_job "$stale_job" Draft Open || fail "could not publish the stale job"
move_job "$live_job"  Draft Open || fail "could not publish the live job"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select expires_at = pickup_window_end from jobs where id = '$stale_job';")" == "t" ]] \
  || fail "the deadline is not the pickup window's end: $("$PSQL" "$DATABASE_URL" -tAc "select expires_at, pickup_window_end from jobs where id = '$stale_job';")"
ok "publication sets the deadline to the pickup date when that is the earlier of the two"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select expires_at between now() + interval '13 days' and now() + interval '15 days'
     from jobs where id = '$live_job';")" == "t" ]] \
  || fail "a job with no pickup date did not get the fourteen-day backstop: $("$PSQL" "$DATABASE_URL" -tAc "select expires_at from jobs where id = '$live_job';")"
ok "a job with no pickup date gets the fourteen-day backstop instead"

# The worker, built and run for real. Its first pass runs immediately at start-up rather than one
# interval later, which is what lets this take seconds instead of five minutes.
pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-worker" ./cmd/worker
go build -o "$WORKDIR/shipper-topics" ./cmd/topics
popd >/dev/null
ok "the worker builds with the jobs domain's task registered"

# The Kafka topic set, applied before anything in this file starts the worker.
#
# cmd/worker is one binary and every start runs *every* registered task, so the three starts below
# also drain the outbox — and until SHIP-135 a publish to a topic nobody created failed that pass
# and left the rows claimable. This section's own assertions never depended on it, which is exactly
# why it went unnoticed for a wave; a run in which three worker starts silently fail every outbox
# pass is not a run demonstrating a working system.
#
# **SHIP-135 owns the topic set and 80-notifications.sh asserts it.** This is a prerequisite rather
# than a check, so it claims no ok(): the point here is only that the system under test is
# provisioned the way a deployment provisions it. The command is idempotent, which is what makes it
# safe to run here and again there.
KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:29092}" \
  "$WORKDIR/shipper-topics" >"$WORKDIR/topics-jobs.log" 2>&1 \
  || { cat "$WORKDIR/topics-jobs.log"; fail "could not apply the Kafka topic set"; }

SHIPPER_ENV=development \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
  "$WORKDIR/shipper-worker" >"$WORKDIR/worker.log" 2>&1 &
worker_pid=$!

for _ in $(seq 1 100); do
  expired_status="$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$stale_job';")"
  [[ "$expired_status" == "Cancelled" ]] && break
  sleep 0.2
done

kill -TERM "$worker_pid" 2>/dev/null || true
for _ in $(seq 1 50); do
  kill -0 "$worker_pid" 2>/dev/null || break
  sleep 0.2
done
wait "$worker_pid" 2>/dev/null || true

[[ "$expired_status" == "Cancelled" ]] \
  || { cat "$WORKDIR/worker.log"; fail "the stale job is $expired_status after a pass, want Cancelled"; }
ok "one pass of the worker ends the job whose pickup date has passed"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$live_job';")" == "Open" ]] \
  || fail "the pass also expired a job whose deadline is thirteen days away"
ok "and leaves alone the one whose deadline has not arrived"

# Through the guard, as the platform. A job that reached Cancelled with no history row would mean
# the sweep had gone round 000402 rather than through it.
expiry_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select h.from_status || '->' || h.to_status || ' ' || h.actor_type || ' ' || (h.actor_id is null)
     from job_status_history h where h.job_id = '$stale_job' and h.to_status = 'Cancelled';")"
[[ "$expiry_row" == "Open->Cancelled system true" ]] \
  || fail "the recorded expiry is '$expiry_row', want 'Open->Cancelled system true'"
ok "the move went through the SHIP-57 guard, recorded as the platform with no account behind it"

expiry_event="$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(string_agg(payload::text, ' '), '') from outbox
    where aggregate_id = '$stale_job' and event_type = 'job.status_changed';")"
# Matched loosely on purpose: jsonb reformats what it stores, so the key order and the spacing in
# the round-tripped text are PostgreSQL's business rather than the payload's.
[[ "$expiry_event" == *Cancelled* && "$expiry_event" == *system* ]] \
  || fail "the expiry emitted no job.status_changed event naming the platform: $expiry_event"
case "$expiry_event" in
  *budget*) fail "the expiry event carries a budget" ;;
esac
ok "it emits the domain event from the domain, inside the same transaction"

grep -q '"stopped cleanly"\|scheduled task stopped' "$WORKDIR/worker.log" \
  || { cat "$WORKDIR/worker.log"; fail "the worker did not stop cleanly on SIGTERM"; }
ok "the worker drains its pass and stops cleanly on SIGTERM"

# And the client sees both halves: the expired job is cancelled, and the live one carries the
# deadline the app needs to warn against (SHIP-69) and to extend (SHIP-70).
status="$(job_get "$jobs_customer_token" "/v1/jobs/$stale_job" "$WORKDIR/jobs-stale-detail.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-stale-detail.json"; fail "reading the expired job returned $status"; }
[[ "$(json "$WORKDIR/jobs-stale-detail.json" '["status"]')" == "cancelled" ]] \
  || fail "the expired job reads as $(json "$WORKDIR/jobs-stale-detail.json" '["status"]')"

status="$(job_get "$jobs_customer_token" "/v1/jobs/$live_job" "$WORKDIR/jobs-live-detail.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-live-detail.json"; fail "reading the live job returned $status"; }
[[ -n "$(json "$WORKDIR/jobs-live-detail.json" '["expires_at"]')" ]] \
  || { cat "$WORKDIR/jobs-live-detail.json"; fail "an Open job does not tell its owner when it expires"; }
ok "the owner sees the expiry through the API — a cancelled job, and a deadline on the live one"


# ---------------------------------------------------------------------------------------
ticket "SHIP-69  a domain event fires forty-eight hours before a job would expire"

# Docs/02 §6.3: "The customer is warned 48 hours before expiry and can extend in one action."
# Demonstrated against the real worker binary, like SHIP-68 above, because the half worth showing is
# the one that only exists as a running process.
#
# # This section reads the outbox, so everything it asserts on is fenced by job id
#
# cmd/worker is one binary and starts every registered task, so each run below also drains the
# outbox as a side effect — which is what broke SHIP-134's section when SHIP-68's met it at the
# wave-4 merge (scripts/verify/80-notifications.sh carries the full account). The recipe is the same
# one that fixed it: **assert only on rows this section can name.** Every query here is keyed on a
# job created here, so nothing another section left behind can satisfy it and nothing this section
# publishes can be mistaken for another section's.
#
# # And the traffic goes the other way too, which is new with this ticket
#
# SHIP-69 adds a second task to that same binary, so **every other section that starts the worker
# now also runs a warning sweep**. That is safe by construction rather than by luck: this section
# and the SHIP-70 one below leave no job inside the forty-eight-hour window — each is either
# already marked warned, or has a deadline days away — so a later worker start claims nothing here.
# A future check that leaves an Open job an hour from its deadline has to know that.

# Two jobs inside the forty-eight-hour window and one outside it. Their deadlines come from 000406's
# trigger on publication rather than from an UPDATE, so the window is measured against a deadline
# the platform computed.
warn_soon="$(python3 -c 'import datetime as d; print((d.datetime.now(d.timezone.utc) + d.timedelta(hours=24)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
warn_later="$(python3 -c 'import datetime as d; print((d.datetime.now(d.timezone.utc) + d.timedelta(days=6)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
warn_distant="$(python3 -c 'import datetime as d; print((d.datetime.now(d.timezone.utc) + d.timedelta(days=20)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"

# expiring_job <name> <pickup-window-end> — a published job whose pickup window closes then.
expiring_job() {
  local out="$WORKDIR/jobs-$1.json" id
  [[ "$(job_request POST "$jobs_customer_token" "verify-jobs-$1-$$" /v1/jobs \
        "{\"goods_description\": \"Pallet for $1\", \"pickup_window\": {\"end\": \"$2\"}}" "$out")" == "201" ]] \
    || { cat "$out"; fail "could not create the job for $1"; }
  id="$(json "$out" '["id"]')"
  move_job "$id" Draft Open || fail "could not publish the job for $1"
  printf '%s' "$id"
}

# run_worker <logfile> — start the real worker, let its first passes run, and stop it cleanly.
#
# The scheduler runs every task once at start-up rather than one interval later, so a start is a
# pass. Stopping before asserting anything means a failed assertion cannot leave a worker running.
run_worker() {
  local log="$1" pid
  SHIPPER_ENV=development \
  LOG_FORMAT=json \
  LOG_LEVEL=debug \
  DATABASE_URL="$DATABASE_URL" \
  REDIS_URL="$REDIS_URL" \
    "$WORKDIR/shipper-worker" >"$log" 2>&1 &
  pid=$!
  sleep 2
  kill -TERM "$pid" 2>/dev/null || true
  for _ in $(seq 1 50); do kill -0 "$pid" 2>/dev/null || break; sleep 0.2; done
  wait "$pid" 2>/dev/null || true
}

warned_job="$(expiring_job warn-soon "$warn_soon")"
unwarned_job="$(expiring_job warn-later "$warn_later")"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select bool_and(expiry_warned_at is null) from jobs where id in ('$warned_job', '$unwarned_job');")" == "t" ]] \
  || fail "a freshly published job is already marked warned"
ok "a job is published unwarned — the mark exists so the warning happens once, not once a pass"

run_worker "$WORKDIR/worker-warning.log"

warned_events="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox where aggregate_id = '$warned_job' and event_type = 'job.expiry_warned';")"
[[ "$warned_events" == "1" ]] \
  || { cat "$WORKDIR/worker-warning.log"; fail "the job a day from its deadline has $warned_events job.expiry_warned events, want 1"; }
ok "one pass emits job.expiry_warned for the job inside the forty-eight-hour window"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox where aggregate_id = '$unwarned_job' and event_type = 'job.expiry_warned';")" == "0" ]] \
  || fail "a job six days from its deadline was warned; the warning would be an announcement"
ok "and leaves alone the one whose deadline is six days away"

# The payload as a consumer reads it out of the outbox: the customer to reach, and both instants.
# And no budget, in any form — an event travels past the last endpoint that could redact anything
# (Docs/01 §4.3).
warned_payload="$("$PSQL" "$DATABASE_URL" -tAc \
  "select payload::text from outbox where aggregate_id = '$warned_job' and event_type = 'job.expiry_warned';")"
[[ "$warned_payload" == *"$jobs_customer_id"* ]] \
  || fail "the warning names no customer to reach: $warned_payload"
[[ "$warned_payload" == *expires_at* && "$warned_payload" == *warned_at* ]] \
  || fail "the warning does not carry both instants: $warned_payload"
case "$warned_payload" in
  *budget*) fail "the warning event carries a budget: $warned_payload" ;;
esac
ok "the payload names the customer and the deadline, and carries no budget"

# Through no transition at all. A job_status_history row would mean the warning had been routed
# through the status guard — an `Open -> Open` move Docs/02 §2 has no row for, appearing in a
# customer's timeline.
warned_state="$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status || ' ' || (j.expiry_warned_at is not null) || ' ' || count(h.id)
     from jobs j left join job_status_history h on h.job_id = j.id
    where j.id = '$warned_job' group by j.status, j.expiry_warned_at;")"
[[ "$warned_state" == "Open true 1" ]] \
  || fail "the warned job is '$warned_state', want 'Open true 1' — Open, marked, and moved by nothing"
ok "the job is still Open, is marked warned, and no transition was recorded for it"

# A second pass, which is what the ticker would do five minutes later.
run_worker "$WORKDIR/worker-warning-again.log"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox where aggregate_id = '$warned_job' and event_type = 'job.expiry_warned';")" == "1" ]] \
  || { cat "$WORKDIR/worker-warning-again.log"; fail "a second pass warned the same job again"; }
ok "a second pass warns nobody again — one notification per deadline, not one per interval"

# ---------------------------------------------------------------------------------------
ticket "SHIP-70  a customer extends an expiring job in one call"

# age_job <job-id> — bring a job's deadline to a day from now, as if it had been listed for
# thirteen.
#
# A plain UPDATE, which is exactly the statement the extend endpoint itself makes: 000402's guard
# passes any update that does not name `status`, and 000406's trigger only ever fills a NULL. So the
# fixture is honest rather than a way around anything — and it is the only way to reach the state
# this ticket is about, since no test can wait thirteen days.
age_job() {
  "$PSQL" "$DATABASE_URL" -q -c \
    "update jobs set expires_at = now() + interval '24 hours' where id = '$1';" >/dev/null
}

# A distant pickup date, so the fourteen days is what ends this job — Docs/02 §6.3's backstop case,
# and the one an extension is actually for.
aged_job="$(expiring_job extend-aged "$warn_distant")"
age_job "$aged_job"
extend_before="$("$PSQL" "$DATABASE_URL" -tAc "select expires_at from jobs where id = '$aged_job';")"

status="$(job_request POST "$jobs_customer_token" "verify-jobs-extend-$$" \
  "/v1/jobs/$aged_job/extend" '{}' "$WORKDIR/jobs-extended.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-extended.json"; fail "extending returned $status, want 200"; }
[[ "$(json "$WORKDIR/jobs-extended.json" '["status"]')" == "open" ]] \
  || fail "the extended job reads as $(json "$WORKDIR/jobs-extended.json" '["status"]'), want open"
ok "one call with an empty body extends the job, and it is still open"

# The column, not the answer the endpoint gave about itself — and fourteen days from *now* rather
# than fourteen added to what it had, which is what stops repeated taps compounding.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select expires_at > '$extend_before'
      and expires_at between now() + interval '13 days' and now() + interval '15 days'
     from jobs where id = '$aged_job';")" == "t" ]] \
  || fail "the deadline is $("$PSQL" "$DATABASE_URL" -tAc "select expires_at from jobs where id = '$aged_job';"), want fourteen days from now"
ok "the stored deadline moved to fourteen days from the moment the customer acted"

# Not a transition. The job has exactly one history row — its publication — and still does.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$aged_job';")" == "1" ]] \
  || fail "the extension recorded a transition; job status is not what an extension changes"
ok "no transition was recorded — Docs/02 §2 has no move for it, and none was invented"

# Docs/02 §6.3's operative rule surviving the escape hatch: an extension never carries a job past
# the date its goods were to be collected.
capped_job="$(expiring_job extend-capped "$warn_later")"
age_job "$capped_job"
status="$(job_request POST "$jobs_customer_token" "verify-jobs-extend-capped-do-$$" \
  "/v1/jobs/$capped_job/extend" '{}' "$WORKDIR/jobs-extend-capped.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-extend-capped.json"; fail "extending returned $status, want 200"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select expires_at = pickup_window_end from jobs where id = '$capped_job';")" == "t" ]] \
  || fail "the extension went past the pickup window's end"
ok "and stops at the pickup window's end when that is the earlier of the two (Docs/02 §6.3)"

# The domain event, which is the only record that an extension happened and what it moved.
extend_event="$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(string_agg(payload::text, ' '), '') from outbox
    where aggregate_id = '$aged_job' and event_type = 'job.expiry_extended';")"
[[ "$extend_event" == *previous_expires_at* ]] \
  || fail "the extension emitted no job.expiry_extended naming the deadline it moved from: $extend_event"
case "$extend_event" in
  *budget*) fail "the extension event carries a budget: $extend_event" ;;
esac
ok "it emits job.expiry_extended carrying both deadlines, and no budget"

# The interlock between the two tickets: a customer who extends must be warned again against the
# deadline they now have. 000407's trigger is what makes that true wherever a deadline moves.
rearm_job="$(expiring_job extend-rearm "$warn_distant")"
age_job "$rearm_job"
"$PSQL" "$DATABASE_URL" -q -c \
  "update jobs set expiry_warned_at = now() where id = '$rearm_job';" >/dev/null
status="$(job_request POST "$jobs_customer_token" "verify-jobs-extend-rearm-do-$$" \
  "/v1/jobs/$rearm_job/extend" '{}' "$WORKDIR/jobs-extend-rearm.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-extend-rearm.json"; fail "extending the warned job returned $status"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select expiry_warned_at is null from jobs where id = '$rearm_job';")" == "t" ]] \
  || fail "the extended job is still marked warned, so it would never be warned again"
ok "extending clears the warning mark, so SHIP-69 fires again against the new deadline"

# The client cannot choose the period. A field this endpoint does not accept is reported rather than
# ignored, because a client that believed it had bought thirty days and received fourteen could not
# tell from a successful response.
status="$(job_request POST "$jobs_customer_token" "verify-jobs-extend-days-$$" \
  "/v1/jobs/$aged_job/extend" '{"days": 30}' "$WORKDIR/jobs-extend-days.json")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/jobs-extend-days.json"; fail "naming a period returned $status, want 400"; }
grep -q 'days' "$WORKDIR/jobs-extend-days.json" || fail "the refusal does not name the field"
ok "a client naming its own period is refused; the platform decides how long"

# A job already bounded by its pickup date gains nothing from more listing time, and is told which
# of its two dates is ending it rather than answered 200 with nothing changed.
bound_job="$(expiring_job extend-bound "$warn_later")"
status="$(job_request POST "$jobs_customer_token" "verify-jobs-extend-bound-do-$$" \
  "/v1/jobs/$bound_job/extend" '{}' "$WORKDIR/jobs-extend-bound.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/jobs-extend-bound.json"; fail "extending a pickup-bound job returned $status, want 409"; }
[[ "$(json "$WORKDIR/jobs-extend-bound.json" '["error"]["code"]')" == "jobs_not_extendable" ]] \
  || { cat "$WORKDIR/jobs-extend-bound.json"; fail "expected code=jobs_not_extendable"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select expires_at = pickup_window_end from jobs where id = '$bound_job';")" == "t" ]] \
  || fail "the refused extension moved the deadline anyway"
ok "a job its pickup date is ending is refused, under a code the app can act on"

# And so is a draft, which has no deadline to extend at all.
draft_to_extend="$(new_draft extend-draft)"
status="$(job_request POST "$jobs_customer_token" "verify-jobs-extend-draft-do-$$" \
  "/v1/jobs/$draft_to_extend/extend" '{}' "$WORKDIR/jobs-extend-draft.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/jobs-extend-draft.json"; fail "extending a draft returned $status, want 409"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select expires_at is null from jobs where id = '$draft_to_extend';")" == "t" ]] \
  || fail "the refused extension gave a draft a deadline"
ok "a draft is refused too — only an Open job has an expiry to extend"

# A stranger's extension is a 404, byte-identical to a job that does not exist. Per endpoint rather
# than per domain: a job is discoverable through whichever route forgets it.
status="$(job_request POST "$jobs_other_token" "verify-jobs-extend-stranger-$$" \
  "/v1/jobs/$aged_job/extend" '{}' "$WORKDIR/jobs-extend-stranger.json")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/jobs-extend-stranger.json"; fail "a stranger's extension returned $status, want 404"; }
status="$(job_request POST "$jobs_other_token" "verify-jobs-extend-nothing-$$" \
  "/v1/jobs/00000000-0000-7000-8000-000000000003/extend" '{}' "$WORKDIR/jobs-extend-nothing.json")"
[[ "$status" == "404" ]] || fail "extending a job that does not exist returned $status, want 404"
python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$WORKDIR/jobs-extend-stranger.json" "$WORKDIR/jobs-extend-nothing.json" \
  || fail "somebody else's job answers differently from no job at all"
ok "a stranger cannot extend, and cannot tell the job apart from one that never existed"

# ---------------------------------------------------------------------------------------
ticket "SHIP-70a  a job with live offers expires on its own deadline, not on its last offer's"

# Docs/02 §2's expiry row reads `Open / Negotiating → Cancelled`. It said `Open` alone until this
# ticket, which was the whole of "a live job" when SHIP-68 and SHIP-69 were written — and stopped
# being so at SHIP-90, when a job with one unanswered offer started sitting at Negotiating. Neither
# sweep could see it, so the deadline was enforced only after the last offer lapsed and returned the
# job to Open.
#
# Every check below runs against the same worker binary the two sections above run, on a job put
# into Negotiating through 000402's own protocol. The distinction that matters is between the
# *status* and the deadline: every job here carries a deadline set by 000406 on publication, and
# only one status of the two was previously visible to either sweep.

negotiating_stale="$(expiring_job negotiating-stale "$warn_distant")"
move_job "$negotiating_stale" Open Negotiating || fail "could not move the job to Negotiating"

# The deadline survived the move — 000406's trigger fills a NULL and never overwrites — which is
# what makes this a claim problem rather than a missing-deadline one.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select expires_at is not null from jobs where id = '$negotiating_stale';")" == "t" ]] \
  || fail "the Negotiating job lost the deadline publication gave it"
ok "a job with live offers keeps the deadline it was published with"

"$PSQL" "$DATABASE_URL" -q -c \
  "update jobs set expires_at = now() - interval '1 hour' where id = '$negotiating_stale';" >/dev/null

run_worker "$WORKDIR/worker-negotiating.log"

negotiating_status="$("$PSQL" "$DATABASE_URL" -tAc \
  "select status from jobs where id = '$negotiating_stale';")"
[[ "$negotiating_status" == "Cancelled" ]] \
  || { cat "$WORKDIR/worker-negotiating.log"; fail "the overdue Negotiating job is $negotiating_status after a pass, want Cancelled"; }
ok "one pass expires it without waiting for its last offer to lapse"

# Through the guard, on the row Docs/02 §2 now names. A `Negotiating->Cancelled` history row is the
# evidence that the widening used the transition table's existing edge rather than inventing one.
negotiating_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select h.from_status || '->' || h.to_status || ' ' || h.actor_type || ' ' || (h.actor_id is null)
     from job_status_history h
    where h.job_id = '$negotiating_stale' and h.to_status = 'Cancelled';")"
[[ "$negotiating_row" == "Negotiating->Cancelled system true" ]] \
  || fail "the recorded expiry is '$negotiating_row', want 'Negotiating->Cancelled system true'"
ok "recorded as Negotiating->Cancelled by the platform, through the SHIP-57 guard"

# The warning claim reads the same set, which is the half of the *Done when* easiest to leave
# behind: a job warned in a status the expiry sweep cannot see is told it is about to die and then
# never dies.
negotiating_warn="$(expiring_job negotiating-warn "$warn_soon")"
move_job "$negotiating_warn" Open Negotiating || fail "could not move the warning job to Negotiating"

run_worker "$WORKDIR/worker-negotiating-warn.log"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox
    where aggregate_id = '$negotiating_warn' and event_type = 'job.expiry_warned';")" == "1" ]] \
  || { cat "$WORKDIR/worker-negotiating-warn.log"; fail "the Negotiating job inside the window was not warned"; }
ok "and the warning sweep reads the same set — a Negotiating job is warned too"

# Still Negotiating and marked warned. A warning moves no job whichever of the two statuses it is
# in, so the second presentation must not have acquired a transition the first never had — and the
# mark is what stops the next pass warning it again.
negotiating_state="$("$PSQL" "$DATABASE_URL" -tAc \
  "select status || ' ' || (expiry_warned_at is not null) from jobs where id = '$negotiating_warn';")"
[[ "$negotiating_state" == "Negotiating true" ]] \
  || fail "the warned job is '$negotiating_state', want 'Negotiating true'"
ok "the warned job is still Negotiating, is marked, and moved by nothing"

# And the customer can act on the warning they were just sent. Docs/02 §6.3 is one mechanism in two
# sentences — warned, *and can extend in one action* — so an extend endpoint still filtering on Open
# alone would answer 409 to the one customer the warning was for.
negotiating_extend="$(expiring_job negotiating-extend "$warn_distant")"
move_job "$negotiating_extend" Open Negotiating || fail "could not move the extend job to Negotiating"
age_job "$negotiating_extend"

status="$(job_request POST "$jobs_customer_token" "verify-jobs-negotiating-extend-do-$$" \
  "/v1/jobs/$negotiating_extend/extend" '{}' "$WORKDIR/jobs-negotiating-extend.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-negotiating-extend.json"; fail "extending a Negotiating job returned $status, want 200"; }
[[ "$(json "$WORKDIR/jobs-negotiating-extend.json" '["status"]')" == "negotiating" ]] \
  || fail "the extended job reads as $(json "$WORKDIR/jobs-negotiating-extend.json" '["status"]'), want negotiating"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select expires_at between now() + interval '13 days' and now() + interval '15 days'
     from jobs where id = '$negotiating_extend';")" == "t" ]] \
  || fail "the deadline did not move: $("$PSQL" "$DATABASE_URL" -tAc "select expires_at from jobs where id = '$negotiating_extend';")"
ok "the customer keeps a job somebody has bid on alive, in one call and with no transition"

# Nothing this section leaves behind is inside the warning window, which is what makes the next
# section's worker run — and every later one — claim none of it. See the SHIP-69 header.
#
# **The predicate covers Negotiating as well as Open since SHIP-70a**, and repeating it here is the
# point: this guard has to cover exactly what the sweeps now claim, or it stops guarding the status
# this file has only just started leaving behind.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from jobs
    where status in ('Open', 'Negotiating') and expiry_warned_at is null
      and expires_at > now() and expires_at <= now() + interval '48 hours';")" == "0" ]] \
  || fail "this file leaves a live job inside the warning window; a later section that starts the worker would warn it"
ok "and no job is left inside the warning window for a later section's worker to pick up"

unset warn_soon warn_later warn_distant warned_job unwarned_job warned_events warned_payload
unset warned_state extend_before extend_event aged_job capped_job rearm_job bound_job draft_to_extend
unset negotiating_stale negotiating_warn negotiating_extend negotiating_row negotiating_state
unset negotiating_status
unset -f expiring_job run_worker age_job

# ---------------------------------------------------------------------------------------
ticket "SHIP-65a  GET /v1/jobs/{id}/history — a job's status history, served to its parties"

# **The first four-segment `GET /v1/jobs/{id}/<literal>` the service has ever served.** While
# `GET /v1/jobs/open/{id}` existed the pair could not be registered at all — both matched
# `/v1/jobs/open/history` with neither more specific, and Go's ServeMux panics at registration, so
# the process did not start. SHIP-83a moved the feed to `/v1/fleet/jobs/{id}`; that the harness
# reaches this endpoint at all is the demonstration, because a regression would not be a failing
# check here but a service that never came up in 10-service.sh.
#
# The fixture is a job with a budget on it, walked through three transitions and cancelled with a
# reason in the customer's own words. Every part of that is load-bearing: three rows make "oldest
# first" a claim with something to be wrong about, the reason is the free text a provider reads, and
# the budget is what Docs/01 §4.3 forbids reaching them.

status="$(job_request POST "$jobs_customer_token" "verify-jobs-history-new-$$" /v1/jobs \
  '{"budget_cents": 432199}' "$WORKDIR/jobs-history-src.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/jobs-history-src.json"; fail "could not create the history job: $status"; }
history_job="$(json "$WORKDIR/jobs-history-src.json" '["id"]')"

# The fixture has something to leak, checked rather than assumed. A privacy check whose job has no
# budget passes forever and proves nothing.
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select budget from jobs where id = '$history_job';")" == "4321.99" ]] \
  || fail "the history job has no budget stored; the disclosure checks below would prove nothing"

move_job "$history_job" Draft Open        || fail "could not publish the history job"
move_job "$history_job" Open Negotiating  || fail "could not move the history job to Negotiating"

status="$(job_request POST "$jobs_customer_token" "verify-jobs-history-cancel-$$" \
  "/v1/jobs/$history_job/cancel" '{"reason": "The goods went with another carrier on Tuesday."}' \
  "$WORKDIR/jobs-history-cancel.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-history-cancel.json"; fail "cancelling the history job returned $status, want 200"; }

history_path="/v1/jobs/$history_job/history"

# jobs_get <token> <path> <name> — one authenticated read. No Idempotency-Key: the middleware lets
# safe methods through untouched, and an endpoint demanding one would ask a client to mint a value
# per read.
jobs_get() {
  curl -s -o "$WORKDIR/jobs-$3.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" "http://localhost:$VERIFY_PORT$2"
}

status="$(curl -s -o "$WORKDIR/jobs-history-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT$history_path")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/jobs-history-anon.json"; fail "an unauthenticated history read returned $status, want 401"; }
ok "it needs a credential and no Idempotency-Key — nothing changes, so there is nothing to absorb"

status="$(jobs_get "$jobs_customer_token" "$history_path" history-owner)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-history-owner.json"; fail "the owner's history read returned $status, want 200"; }

python3 - "$WORKDIR/jobs-history-owner.json" <<'PY' || fail "the owner's history is not what SHIP-65a asks for"
import json, sys

page = json.load(open(sys.argv[1]))

if page.get("has_more") is not False or page.get("next_cursor") is not None:
    print("the history paged or reported itself truncated:", page, file=sys.stderr)
    sys.exit(1)

moves = [(e["from_status"], e["to_status"]) for e in page["data"]]
want = [("draft", "open"), ("open", "negotiating"), ("negotiating", "cancelled")]
if moves != want:
    print("the history reads", moves, "want", want, "oldest first", file=sys.stderr)
    sys.exit(1)

for entry in page["data"]:
    for field in ("id", "job_id", "actor_type", "recorded_at", "accepted_at"):
        if not entry.get(field):
            print("an entry is missing", field, ":", entry, file=sys.stderr)
            sys.exit(1)
    if entry["actor_type"] != "customer":
        print("an entry is attributed to", entry["actor_type"], file=sys.stderr)
        sys.exit(1)
    # The actor identifier is deliberately absent — the actor is served as its kind. See the
    # getJobHistory description for the four separate reasons, one per kind of actor.
    if "actor_id" in entry:
        print("an entry names the actor:", entry, file=sys.stderr)
        sys.exit(1)

if page["data"][-1].get("reason") != "The goods went with another carrier on Tuesday.":
    print("the cancellation reason did not survive:", page["data"][-1], file=sys.stderr)
    sys.exit(1)

# Absent rather than empty on the entries nobody gave one for, so a client can tell "gave no
# reason" from "gave an empty one".
if any("reason" in entry for entry in page["data"][:-1]):
    print("an entry with no reason carries the key:", page["data"], file=sys.stderr)
    sys.exit(1)
PY
ok "the owner reads every recorded transition, oldest first, with its actor, its reason and both clocks"

# The provider is asked *before* the bid exists and again after it, with one account. That is what
# makes this a check on the bid rather than on the fixture: the same credential, the same job, and
# the only thing that changed is a row in a table this domain may not import.
jobs_history_provider_id="$(json "$WORKDIR/jobs-provider.json" '["id"]')"

status="$(jobs_get "$jobs_provider_token" "$history_path" history-nobid)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/jobs-history-nobid.json"; fail "a provider who has not bid got $status, want 404"; }

# 'Rejected', deliberately: SHIP-65a serves the history to a provider holding a bid **at any
# status**, and a predicate that filtered on 'Accepted' or 'Submitted' would pass every unit test
# and lock the losing bidders out of the one endpoint they need.
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 \
  -v job="$history_job" -v provider="$jobs_history_provider_id" >/dev/null <<'SQL'
INSERT INTO bids (id, job_id, provider_id, status, amount)
VALUES (gen_random_uuid(), :'job', :'provider', 'Rejected', 185.00);
SQL

status="$(jobs_get "$jobs_provider_token" "$history_path" history-provider)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/jobs-history-provider.json"; fail "the bidding provider's history read returned $status, want 200"; }
ok "a provider holding a rejected bid reads it, and the same provider could not a moment earlier"

diff -q "$WORKDIR/jobs-history-owner.json" "$WORKDIR/jobs-history-provider.json" >/dev/null \
  || fail "the two parties see different histories; SHIP-65a serves both the same rows"
ok "both parties read the same rows — there is no per-reader field, so there is none to get wrong"

# The disclosure rule, and it is stronger than "both are 404". A body that differs in what it *says*
# — a different message, a different code — tells a stranger holding an identifier that somebody
# else's job exists.
#
# The comparison is over `(code, message)` and deliberately not `diff` over the whole body, which is
# the shape every other refusal check in this file already uses. `Docs/10` §4.6 puts the request id
# **inside** the error object, so two refusals are never byte-identical and never can be: the field
# is unique per request by design. A `diff` here fails on a service that is behaving perfectly, and
# says "distinguishable" about the one field a stranger learns nothing from.
jobs_same_refusal() {
  python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$1" "$2"
}

missing_job="$(python3 -c 'import uuid; print(uuid.uuid4())')"
absent_status="$(jobs_get "$jobs_customer_token" "/v1/jobs/$missing_job/history" history-absent)"
[[ "$absent_status" == "404" ]] || { cat "$WORKDIR/jobs-history-absent.json"; fail "a job that does not exist returned $absent_status, want 404"; }

status="$(jobs_get "$jobs_other_token" "$history_path" history-stranger)"
[[ "$status" == "$absent_status" ]] || { cat "$WORKDIR/jobs-history-stranger.json"; fail "another customer got $status and a missing job gets $absent_status"; }
jobs_same_refusal "$WORKDIR/jobs-history-stranger.json" "$WORKDIR/jobs-history-absent.json" \
  || { printf '  stranger: '; cat "$WORKDIR/jobs-history-stranger.json"; printf '\n  missing:  '; cat "$WORKDIR/jobs-history-absent.json"; \
       fail "a stranger's refusal is distinguishable from a job that does not exist"; }
jobs_same_refusal "$WORKDIR/jobs-history-nobid.json" "$WORKDIR/jobs-history-absent.json" \
  || fail "a provider who has not bid gets a refusal distinguishable from a job that does not exist"
unset -f jobs_same_refusal
ok "anybody else gets exactly what a missing job gets — same code, same message; a 403 would confirm the job exists"

# The budget, on the response a **provider** was served, and checked in four ways because three of
# them are individually defeatable. Waves 10 and 11 each isolated a sentence carrying no field, no
# value and no digit that passed a closed key set, a word search and a value search at once.
python3 - "$WORKDIR/jobs-history-provider.json" <<'PY' || fail "the provider's history discloses the customer's budget"
import json, re, sys

raw = open(sys.argv[1]).read()
page = json.loads(raw)

# Not vacuous: the response has to be carrying the history before "it carries no budget" means
# anything at all.
if not page.get("data") or "another carrier" not in raw:
    print("this response carries no history, so it proves nothing:", raw, file=sys.stderr)
    sys.exit(1)

allowed = {"data", "next_cursor", "has_more", "id", "job_id", "from_status", "to_status",
           "actor_type", "reason", "recorded_at", "accepted_at"}

def walk(node, path):
    if isinstance(node, dict):
        for key, child in node.items():
            if key not in allowed:
                print("the provider's history carries", repr(key), "at", path, file=sys.stderr)
                sys.exit(1)
            walk(child, path + "." + key)
    elif isinstance(node, list):
        for i, child in enumerate(node):
            walk(child, "%s[%d]" % (path, i))

walk(page, "$")

if "budget" in raw.lower():
    print("the word \"budget\" appears in a provider's response:", raw, file=sys.stderr)
    sys.exit(1)

# Identifiers are removed first: a UUID is hexadecimal, so a run of digits can occur inside one by
# chance — rarely enough to pass in review and often enough to fail in CI one morning.
searchable = re.sub(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}",
                    "<id>", raw)
for rendering in ("4321.99", "432199", "4321,99", "4,321.99", "4321"):
    if rendering in searchable:
        print("the budget appears as", rendering, "in", raw, file=sys.stderr)
        sys.exit(1)

# And not as a sentence, which is the check the other three cannot make. `reason` is free text an
# actor wrote and a provider reads, so this is the form a "budget supplied" signal would take here.
prose = ["maximum", "max ", "ceiling", "cap ", "willing to pay", "price range", "up to $",
         "has set a", "has a limit", "limit of"]
lowered = raw.lower()
for phrase in prose:
    if phrase in lowered:
        print("a provider's history contains", repr(phrase), "in", raw, file=sys.stderr)
        sys.exit(1)

# The check on the check: the guard has to fire on the sentence waves 10 and 11 isolated, or it is
# a list of phrases nothing could ever contain.
smuggled = 'the goods went with another carrier. The customer has set a maximum.'
if not any(phrase in smuggled.lower() for phrase in prose):
    print("the prose guard does not fire on the sentence it exists for", file=sys.stderr)
    sys.exit(1)
PY
ok "no budget reaches the provider as a key, a word, a value, or a sentence — and the prose guard fires on the sentence it exists for"

# The proof that the identifier slot is an identifier again (SHIP-83a). "open" is now parsed as a
# job id and refused as one, which is a 400 rather than the 404 the old literal route answered.
# **This is not a defect to fix**: it is what four segments under `/v1/jobs/{id}/` cost, and this
# section is the first endpoint to spend it.
status="$(jobs_get "$jobs_customer_token" /v1/jobs/open history-open-literal)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/jobs-history-open-literal.json"; fail "GET /v1/jobs/open returned $status, want 400 — 'open' is an identifier now"; }
ok "GET /v1/jobs/open answers 400 — the {id} slot holds an identifier again, which is what freed this path"

unset history_job history_path missing_job absent_status jobs_history_provider_id
unset -f jobs_get
