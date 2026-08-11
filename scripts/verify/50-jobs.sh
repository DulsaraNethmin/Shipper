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
