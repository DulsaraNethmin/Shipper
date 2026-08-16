# shellcheck shell=bash
#
# M3 bidding — what a provider offers against a job, and the two rules that make "once" true.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 60–69 is the fleet and bidding range, and 60-fleet.sh has the lower half. This file is bidding's to
# append to and no other track's to edit — which is the whole reason the script was split
# (Docs/11 §9, SHIP-15e).
#
# # Mobile numbers are a shared namespace across every section, and this is the corrected map
#
# Sections are *sourced* into one process, and every account registered anywhere needs a unique
# mobile — so two sections drawing the same prefix collide on the phone unique index. Docs/11 §9
# records that a track lost twenty minutes to it, and that the only copy of the allocation was a
# comment in 90-admin.sh's header which did not mention this file at all.
#
#   04120  identity      0413x  jobs        0414x  fleet
#   0416x  bidding       0417x  delivery    04180  outbox      0419x  admin
#
# **The three accounts below draw 04145, 04146 and 04147, which are inside fleet's block.** They are
# free only because 60-fleet.sh stopped at 04144, which is a coincidence rather than a guarantee — one
# more vehicle in that file would end it. They are left where they are because renumbering a working
# fixture is a change with no test behind it; **anything new in this file takes 0416x**, and SHIP-92's
# accounts are the first to do so.
#
# # This section registers its own accounts rather than reusing 60-fleet.sh's
#
# Sections are sourced, so everything 60-fleet.sh left behind is in scope, and using it would be
# permitted. It is not done, on the runner's own advice: a section that needs an account registers
# one. 60-fleet.sh leaves its eligibility provider having bid on nothing and its job in a state that
# file controls, and "this provider has exactly one live offer" cannot be demonstrated against a
# fixture this section does not own.
#
# # Every token here claims `role: customer`, including the ones that succeed
#
# mint_token puts `role: customer` in every token it issues, which is exactly the thing worth
# exercising: the platform decides what a caller may do by reading the database, not by believing the
# claim. So the provider below bids with a token that says customer, and the customer is refused with
# an identical one — a stronger demonstration than two honest tokens would be.
#
# No check here signs in, so SHIP-47's per-address bucket (rl:v1:signin:*, shared across sections and
# across runs from 127.0.0.1) is untouched by this file.
#
# Nothing here reads Kafka. **Two of the last three sections do more than send requests**, and both
# say so where they sit: SHIP-89's starts the real `cmd/worker` binary, and SHIP-136's reads the
# outbox. Both are fenced on the accounts registered below rather than counted over a table, because
# this database persists between runs.
#
# **The outbox rows this file writes are left unpublished on purpose**, for 80-notifications.sh to
# drain and read off `shipper.bid`. That is why the worker start in the SHIP-89 section is pointed at
# a broker that is not there — a drain here would publish them early and quietly empty another
# section's assertion. The reasoning is written out in full in that section's own header.

# --- the accounts and the job these checks run against -----------------------------------------

status="$(post_json "verify-bid-prov-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"bid-provider-$$@example.com\",\"phone\":\"04145$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/bid-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-provider.json"; fail "could not register the bidding provider: $status"; }
bid_provider_id="$(json "$WORKDIR/bid-provider.json" '["id"]')"
bid_provider_token="$(mint_token "$bid_provider_id")"

status="$(post_json "verify-bid-rival-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"bid-rival-$$@example.com\",\"phone\":\"04146$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/bid-rival.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-rival.json"; fail "could not register the rival provider: $status"; }
bid_rival_id="$(json "$WORKDIR/bid-rival.json" '["id"]')"
bid_rival_token="$(mint_token "$bid_rival_id")"

status="$(post_json "verify-bid-cust-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"bid-customer-$$@example.com\",\"phone\":\"04147$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/bid-customer.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-customer.json"; fail "could not register the bidding customer: $status"; }
bid_customer_id="$(json "$WORKDIR/bid-customer.json" '["id"]')"
bid_customer_token="$(mint_token "$bid_customer_id")"

# bid_post <token> <key> <path> <body> <name> — one authenticated state-changing request, keeping
# the response headers.
#
# The headers are not decoration: `Idempotency-Replayed` is how a check says *which* mechanism
# answered a retry, and asserting on the status alone could not tell Redis from the database.
bid_post() {
  curl -s -X POST -o "$WORKDIR/bid-$5.json" -D "$WORKDIR/bid-$5.headers" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$4" \
    "http://localhost:$VERIFY_PORT$3"
}

# replayed_from_redis <name> — whether the middleware answered that request from its own store.
replayed_from_redis() { grep -qi '^idempotency-replayed: true' "$WORKDIR/bid-$1.headers"; }

# forget_the_cached_response <subject> <key> — delete the middleware's entry, as a TTL expiry would.
#
# The key is namespaced by the authenticated subject (SHIP-44), which is what stops one client
# reading another's stored response.
forget_the_cached_response() {
  redis-cli -u "$REDIS_URL" del "idem:v1:user:$1:$2" >/dev/null
}

# move_job <job> <from> <to> — a status transition made the only way 000402 permits one: a
# job_status_history row written in the same transaction and named by shipper.job_status_transition.
#
# SHIP-63's publish endpoint does not exist yet, so the guard is satisfied directly rather than
# bypassed. The same helper 60-fleet.sh defines, with this section's customer as the actor — repeated
# rather than reused so that neither file's fixture can be broken by an edit to the other's.
#
# **`<from>` may be `-`, meaning "wherever it is now" (SHIP-90).** Every call in this file named its
# own `from` and every one of them was right until offers started moving jobs to Negotiating; after
# that a fixture closing a job it had already bid on failed inside the helper, on a guard message
# about a history row, which is a long way from the cause. A call that is arranging a job *before*
# any offer exists still names the status, because there the `from` is the point.
move_job() {
  local from="$2"
  if [[ "$from" == "-" ]]; then
    from="$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$1';" | tr -d ' ')"
  fi
  set -- "$1" "$from" "$3"
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 <<SQL
DO \$do\$
DECLARE entry uuid := gen_random_uuid();
BEGIN
  INSERT INTO job_status_history
      (id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
  VALUES (entry, '$1', '$2', '$3', 'customer', '$bid_customer_id', now());
  PERFORM set_config('shipper.job_status_transition', entry::text, true);
  UPDATE jobs SET status = '$3' WHERE id = '$1';
END
\$do\$;
SQL
}

# Both providers meet Docs/04 §3's automated baseline, serve Victoria, and run a truck that fits.
# Driving the real verification endpoints would need the token out of the console email and the OTP
# out of the log, which 40-identity.sh already demonstrates; repeating it here would be testing
# identity rather than bidding.

# verify_provider <user-id>… — move a provider's verification record to Verified (SHIP-81a).
#
# **Added by SHIP-81a, which is the minimum this section needed and nothing else.** Docs/04 §4's five
# outcomes now exist as a record, every provider starts Pending, and only Verified may bid — so the
# baseline update below is no longer the whole of "this provider may bid", and without this every
# check in this file would fail on a 403 that is the platform working correctly.
#
# It goes through `provider_verification_decide` because that is the only thing that can move the
# state: `provider_verification_change_is_guarded` refuses a direct UPDATE, so a fixture that tried
# one would fail here rather than in the check it was setting up.
verify_provider() {
  local id
  for id in "$@"; do
    "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
      "select provider_verification_decide('$id', 'Verified', 'system', null,
                                           'the bidding verify section');" >/dev/null
  done
}

"$PSQL" "$DATABASE_URL" -q -c \
  "update users set email_verified_at = now(), phone_verified_at = now()
     where id in ('$bid_provider_id', '$bid_rival_id');"
verify_provider "$bid_provider_id" "$bid_rival_id"

for pair in "$bid_provider_token:BID101" "$bid_rival_token:BID102"; do
  status="$(bid_post "${pair%%:*}" "verify-bid-area-${pair##*:}-$$" /v1/fleet/vehicles \
    "{\"registration\":\"${pair##*:}\",\"vehicle_type\":\"box_truck\",\"max_weight_kg\":1200,\"load_length_cm\":300,\"load_width_cm\":160,\"load_height_cm\":180}" \
    "vehicle-${pair##*:}")"
  [[ "$status" == "201" ]] || { cat "$WORKDIR/bid-vehicle-${pair##*:}.json"; fail "adding ${pair##*:} returned $status"; }

  status="$(curl -s -X PATCH -o "$WORKDIR/bid-profile.json" -w '%{http_code}' \
    -H "$auth_header: Bearer ${pair%%:*}" -H "Idempotency-Key: verify-bid-prof-${pair##*:}-$$" \
    -H 'Content-Type: application/json' -d '{"service_area":{"states":["VIC"]}}' \
    "http://localhost:$VERIFY_PORT/v1/fleet/profile")"
  [[ "$status" == "200" ]] || { cat "$WORKDIR/bid-profile.json"; fail "declaring VIC returned $status"; }
done

# The job carries a budget, deliberately, and it is the number the privacy checks below search for.
# A job with no budget would make every one of those assertions vacuous — a privacy check whose
# fixture has nothing to leak passes forever and proves nothing.
status="$(bid_post "$bid_customer_token" "verify-bid-job-$$" /v1/jobs \
  '{"pickup":{"line":"5 Church Street","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"1 Bourke Street","suburb":"Melbourne","state":"VIC","postcode":"3000"},
    "goods_description":"Two-seater sofa","weight_kg":80,"length_cm":190,"width_cm":90,"height_cm":80,
    "budget_cents":432199}' job)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-job.json"; fail "creating the bidding job returned $status"; }
bid_job_id="$(json "$WORKDIR/bid-job.json" '["id"]')"
move_job "$bid_job_id" Draft Open

# The offer every check below sends, unless it is breaking one field of it. The timestamps carry an
# offset rather than a Z, because that is what a phone in Melbourne sends.
bid_pickup="$(date -u -v+2d '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d '+2 days' '+%Y-%m-%dT%H:%M:%SZ')"
bid_deliver="$(date -u -v+3d '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d '+3 days' '+%Y-%m-%dT%H:%M:%SZ')"
bid_body="{\"amount_cents\":45000,\"pickup_at\":\"$bid_pickup\",\"deliver_by\":\"$bid_deliver\",\"message\":\"Can collect from the loading dock.\"}"

# ---------------------------------------------------------------------------------------
ticket "SHIP-84  POST /v1/jobs/{id}/bids — a verified, eligible provider bids with price and timing"

status="$(curl -s -X POST -o "$WORKDIR/bid-anon.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-bid-anon-$$" -H 'Content-Type: application/json' \
  -d "$bid_body" "http://localhost:$VERIFY_PORT/v1/jobs/$bid_job_id/bids")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/bid-anon.json"; fail "an unauthenticated bid returned $status, want 401"; }
ok "it cannot be reached without a credential"

status="$(curl -s -X POST -o "$WORKDIR/bid-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $bid_provider_token" -H 'Content-Type: application/json' \
  -d "$bid_body" "http://localhost:$VERIFY_PORT/v1/jobs/$bid_job_id/bids")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-nokey.json"; fail "a bid with no Idempotency-Key returned $status, want 400"; }
ok "and not without an Idempotency-Key — the key is what the offer is recorded under, not just how a retry is absorbed"

status="$(bid_post "$bid_provider_token" "verify-bid-place-$$" "/v1/jobs/$bid_job_id/bids" "$bid_body" place)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-place.json"; fail "placing a bid returned $status, want 201"; }
bid_id="$(json "$WORKDIR/bid-place.json" '["id"]')"
[[ "$bid_id" =~ ^[0-9a-f-]{36}$ ]] || fail "the response carries no bid id: $bid_id"
[[ "$(json "$WORKDIR/bid-place.json" '["status"]')" == "submitted" ]] \
  || fail "the bid came back as $(json "$WORKDIR/bid-place.json" '["status"]'), want the lower snake case wire form"
[[ "$(json "$WORKDIR/bid-place.json" '["amount_cents"]')" == "45000" ]] || fail "the amount did not come back"
[[ "$(json "$WORKDIR/bid-place.json" '["job_id"]')" == "$bid_job_id" ]] || fail "the bid names the wrong job"
ok "a verified, eligible provider places an offer with a price and two commitments about timing"

# The row, not the answer the endpoint gave about itself. The amount is read as a numeric, which is
# where a one-directional cents conversion would show: Go holds 45000 and the column holds 450.00.
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select provider_id || ' ' || status || ' ' || amount::text || ' ' || idempotency_key
     from bids where id = '$bid_id';")"
[[ "$stored" == "$bid_provider_id Submitted 450.00 verify-bid-place-$$" ]] \
  || fail "the stored bid is '$stored', want '$bid_provider_id Submitted 450.00 verify-bid-place-$$'"
ok "the row is owned by the calling provider, is Submitted, and holds 45000 cents as 450.00"

# ---------------------------------------------------------------------------------------
ticket "SHIP-84  the customer's budget reaches no provider, in any form"

# The fixture, verified rather than assumed.
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select budget from jobs where id = '$bid_job_id';")" == "4321.99" ]] \
  || fail "the job has no budget, so the checks below assert nothing"

python3 - "$WORKDIR/bid-place.json" <<'PY' || fail "a provider's own bid carries something of the customer's"
import json, re, sys

# **A closed set of keys, not a search for the word "budget".** Docs/01 §4.3 forbids the customer's
# maximum reaching a provider "as an amount, a band, or a 'budget supplied' flag", and a deny-list of
# names cannot express that: `max_price` is a budget and does not contain the word. This is the other
# direction — anything not promised is refused — and it is the same list as the `Bid` schema in
# contracts/paths/bidding.yaml and providerBidKeys in internal/bidding/http_test.go.
allowed = {
    "id", "job_id", "status", "offered_by", "amount_cents",
    "pickup_at", "deliver_by", "message", "superseded_by",
    "created_at", "updated_at",
    # The collection envelope of Docs/10 §4.5, which the history endpoint answers with (SHIP-88).
    "data", "next_cursor", "has_more",
}

def keys(node):
    if isinstance(node, dict):
        for key, child in node.items():
            yield key
            yield from keys(child)
    elif isinstance(node, list):
        for child in node:
            yield from keys(child)

identifier = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")

raw = open(sys.argv[1]).read()

unexpected = sorted(set(keys(json.loads(raw))) - allowed)
if unexpected:
    print(sys.argv[1], "carries keys this API never promised a provider:", unexpected, file=sys.stderr)
    print("A budget under another name is still a budget (Docs/01 §4.3).", file=sys.stderr)
    sys.exit(1)

if "budget" in raw.lower():
    print(sys.argv[1], "mentions the budget:", raw, file=sys.stderr)
    sys.exit(1)

# Identifiers come out before the value search: a UUID is hexadecimal, so a run of digits can occur
# inside one by chance — rarely enough to pass review and often enough to fail one morning.
searchable = identifier.sub("<id>", raw)
for rendering in ("4321.99", "432199", "4,321.99"):
    if rendering in searchable:
        print(sys.argv[1], "carries the budget's value as", rendering, file=sys.stderr)
        print(raw, file=sys.stderr)
        sys.exit(1)
PY
ok "the bid response is a closed set of keys, and carries neither the word nor the value nor a key under another name"

# Nothing of the job travels in a bid at all, which is stronger than redaction: there is no field to
# leak because there is no job in the shape. GET /v1/fleet/jobs/{id} is where a provider reads the job.
for disclosure in 'Church Street' 'Richmond' 'sofa' '"weight' '"pickup"'; do
  grep -q "$disclosure" "$WORKDIR/bid-place.json" \
    && { cat "$WORKDIR/bid-place.json"; fail "the bid carries '$disclosure' — a bid names its job and nothing else"; }
done
ok "a bid names the job it is against and copies nothing out of it"

# ---------------------------------------------------------------------------------------
ticket "SHIP-84  one live offer per provider per job, held by a partial unique index"

status="$(bid_post "$bid_provider_token" "verify-bid-second-$$" "/v1/jobs/$bid_job_id/bids" "$bid_body" second)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-second.json"; fail "a second offer returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-second.json" '["error"]["code"]')" == "bidding_already_bid" ]] \
  || { cat "$WORKDIR/bid-second.json"; fail "expected code=bidding_already_bid"; }
ok "a second offer under a fresh key is refused with a code the app can act on"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids where provider_id = '$bid_provider_id' and job_id = '$bid_job_id';")" == "1" ]] \
  || fail "the refused second offer left a row behind"
ok "and wrote nothing — the index refused it, not a check that could race"

# The rule is per provider. A marketplace where the second provider to bid was refused would be no
# marketplace at all.
status="$(bid_post "$bid_rival_token" "verify-bid-rival-place-$$" "/v1/jobs/$bid_job_id/bids" "$bid_body" rival)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-rival.json"; fail "a second provider was refused a job the first had bid on: $status"; }
ok "another provider may bid on the same job — the rule is per provider, not per job"

# ---------------------------------------------------------------------------------------
ticket "SHIP-84  a provider never sees another provider's offer or its price"

# **Both providers deliberately send the same key.** Clients generate their own; two colliding is
# unlikely and is not something this API may rely on, and reusing one is how a competitor would probe
# for a store read scoped by job and key but not by provider. Every key in the leaked response would
# be one this API promises a provider, so the budget check above would pass on it.
shared_key="verify-bid-shared-$$"

status="$(bid_post "$bid_customer_token" "verify-bid-job2-$$" /v1/jobs \
  '{"pickup":{"line":"12 Swan Street","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"400 Collins Street","suburb":"Melbourne","state":"VIC","postcode":"3000"},
    "goods_description":"Filing cabinet","weight_kg":40,"budget_cents":432199}' job2)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-job2.json"; fail "creating the second job returned $status"; }
second_job="$(json "$WORKDIR/bid-job2.json" '["id"]')"
move_job "$second_job" Draft Open

# A distinctive price, so the rival's response can be searched for it.
status="$(bid_post "$bid_provider_token" "$shared_key" "/v1/jobs/$second_job/bids" \
  "{\"amount_cents\":777701,\"pickup_at\":\"$bid_pickup\",\"deliver_by\":\"$bid_deliver\"}" mine)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-mine.json"; fail "the first provider's offer returned $status"; }

status="$(bid_post "$bid_rival_token" "$shared_key" "/v1/jobs/$second_job/bids" "$bid_body" theirs)"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/bid-theirs.json"; fail "the rival's key was refused by somebody else's bid: $status"; }
[[ "$(json "$WORKDIR/bid-theirs.json" '["id"]')" != "$(json "$WORKDIR/bid-mine.json" '["id"]')" ]] \
  || fail "the rival was handed the first provider's bid — the store read is not scoped by provider"
grep -q '777701' "$WORKDIR/bid-theirs.json" \
  && { cat "$WORKDIR/bid-theirs.json"; fail "the rival's response carries the first provider's price"; }
[[ "$(json "$WORKDIR/bid-theirs.json" '["amount_cents"]')" == "45000" ]] || fail "the rival did not get their own offer"
ok "two providers sharing one idempotency key each get their own offer, and neither sees the other's price"

# ---------------------------------------------------------------------------------------
ticket "SHIP-84  a retry is answered from the record, not refused as a second bid"

# # This is where the two idempotency mechanisms are told apart
#
# The middleware replays a *response* while its Redis entry lives; the column replays the *row*
# forever. A phone out of signal for a day outlives any TTL worth setting, and without the stored key
# that request would meet uq_bids_one_submitted_per_provider_per_job instead and be told "you already
# have a live offer" — a 409 for a request that actually succeeded.
retry_key="verify-bid-retry-$$"
retry_job_body="{\"amount_cents\":50000,\"pickup_at\":\"$bid_pickup\",\"deliver_by\":\"$bid_deliver\"}"

status="$(bid_post "$bid_customer_token" "verify-bid-job3-$$" /v1/jobs \
  '{"pickup":{"line":"30 Bridge Road","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"1 Malop Street","suburb":"Geelong","state":"VIC","postcode":"3220"},
    "goods_description":"Pallet of tiles","weight_kg":300}' job3)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-job3.json"; fail "creating the retry job returned $status"; }
retry_job="$(json "$WORKDIR/bid-job3.json" '["id"]')"
move_job "$retry_job" Draft Open

status="$(bid_post "$bid_provider_token" "$retry_key" "/v1/jobs/$retry_job/bids" "$retry_job_body" r1)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-r1.json"; fail "the first offer returned $status, want 201"; }
replayed_from_redis r1 && fail "the first request was answered from the cache"
retry_bid_id="$(json "$WORKDIR/bid-r1.json" '["id"]')"
ok "the offer is placed"

status="$(bid_post "$bid_provider_token" "$retry_key" "/v1/jobs/$retry_job/bids" "$retry_job_body" r2)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-r2.json"; fail "the cached retry returned $status, want the stored 201"; }
replayed_from_redis r2 || fail "the second request was not replayed by the middleware"
diff -q "$WORKDIR/bid-r1.json" "$WORKDIR/bid-r2.json" >/dev/null \
  || fail "the replayed response is not byte-identical to the original"
ok "a retry inside the cache is replayed by the middleware, byte for byte, as the original 201"

# Now remove the entry, as a TTL expiry, an eviction or a failover would. The request runs all the
# way to the table this time.
forget_the_cached_response "$bid_provider_id" "$retry_key"

status="$(bid_post "$bid_provider_token" "$retry_key" "/v1/jobs/$retry_job/bids" "$retry_job_body" r3)"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/bid-r3.json"; fail "a retry after the cache forgot it returned $status, want 200 from the row"; }
replayed_from_redis r3 && fail "the third request was still answered from the cache"
[[ "$(json "$WORKDIR/bid-r3.json" '["id"]')" == "$retry_bid_id" ]] \
  || fail "the retry answered with a different bid: $(json "$WORKDIR/bid-r3.json" '["id"]')"
ok "and a retry the cache has forgotten is answered from the row — 200, the same offer, no 409"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids where provider_id = '$bid_provider_id' and job_id = '$retry_job';")" == "1" ]] \
  || fail "three requests under one key left more than one bid"
ok "one offer at the end of all three — Redis makes the retry cheap, the index makes it correct"

# ---------------------------------------------------------------------------------------
ticket "SHIP-84  who may bid is the platform's decision, and a refusal discloses nothing"

# CLAUDE.md: no authorisation decision on the device. The app hides the bid form from a customer; the
# platform refuses it — and the token this customer presents is the same shape as the provider's.
status="$(bid_post "$bid_customer_token" "verify-bid-cust-place-$$" "/v1/jobs/$bid_job_id/bids" "$bid_body" cust)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-cust.json"; fail "the job's own customer bid on it: $status"; }
ok "a customer is refused, including the customer who owns the job"

# A job that exists and is no longer biddable answers exactly what a job that never existed answers.
# 403 would confirm the job is there, and what became of work a provider was not given is not
# something this API discloses.
move_job "$bid_job_id" - Cancelled
status="$(bid_post "$bid_rival_token" "verify-bid-closed-$$" "/v1/jobs/$bid_job_id/bids" "$bid_body" closed)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-closed.json"; fail "a bid on a cancelled job returned $status, want 404"; }

status="$(bid_post "$bid_rival_token" "verify-bid-nothing-$$" \
  /v1/jobs/00000000-0000-7000-8000-000000000084/bids "$bid_body" nothing)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-nothing.json"; fail "a bid on a job that does not exist returned $status"; }

python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$WORKDIR/bid-closed.json" "$WORKDIR/bid-nothing.json" \
  || fail "a job that is no longer biddable answers differently from a job that does not exist"
ok "a job nobody may bid on answers exactly what a missing job answers — the refusal confirms nothing"

# An unverified provider is refused too, which is the "verified" in the *Done when*. Broken and
# restored, so the rest of this file still runs against an eligible fleet.
"$PSQL" "$DATABASE_URL" -q -c "update users set phone_verified_at = null where id = '$bid_rival_id';"
status="$(bid_post "$bid_rival_token" "verify-bid-unverified-$$" "/v1/jobs/$retry_job/bids" "$bid_body" unverified)"
"$PSQL" "$DATABASE_URL" -q -c "update users set phone_verified_at = now() where id = '$bid_rival_id';"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-unverified.json"; fail "an unverified provider bid: $status"; }
ok "an unverified provider is refused — the eligibility filter is SHIP-81's, read through a port rather than copied"

# ---------------------------------------------------------------------------------------
ticket "SHIP-90  a job with active offers presents as Negotiating, without closing to new bids"

# SHIP-90's *Done when*, and the second half is the ticket. Docs/02 §1: "'Negotiating' is a useful
# presentation status. Technically, the job remains available for eligible bids unless the customer
# closes it or awards a bid."
#
# $retry_job has carried the retry section's offer since above, so it is already at Negotiating — put
# there by the platform when that offer was placed, rather than by `move_job` as this check used to
# do. **The status is read before anything else here**, because a filter accepting only Open would
# pass every other check in this file and surface months from now as jobs silently refusing bids the
# moment somebody negotiated.
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$retry_job';")" == "Negotiating" ]] \
  || fail "the job is $("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$retry_job';") after an offer was placed on it, want Negotiating"
ok "an offer moved the job to Negotiating, and nothing in this file asked it to"

status="$(bid_post "$bid_rival_token" "verify-bid-negotiating-$$" "/v1/jobs/$retry_job/bids" "$bid_body" negotiating)"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/bid-negotiating.json"; fail "a Negotiating job refused a bid ($status). Docs/02 §1 keeps it open to eligible bids."; }
ok "and a second eligible provider still bids on it — Negotiating does not close a job to offers"

# The move is the platform's and it went through the guard, which is the half a status read cannot
# see: 000402 refuses a status write with no job_status_history row describing it, so a move made any
# other way would have failed rather than arrived quietly.
negotiating_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select from_status || ' ' || actor_type || ' ' || (actor_id is null)::text || ' ' || (reason is not null)::text
     from job_status_history where job_id = '$retry_job' and to_status = 'Negotiating';")"
[[ "$negotiating_row" == "Open system true true" ]] \
  || fail "the recorded move is '$negotiating_row', want 'Open system true true' — the platform, with no account behind it and a reason"
ok "recorded through the SHIP-57 guard as the platform, with a reason a customer's timeline can show"

# ---------------------------------------------------------------------------------------
ticket "SHIP-90  a second offer does not move the job again, and a rival's offer keeps it there"

# $second_job has carried two live offers since the privacy section above. Docs/02 §2 says "**first**
# bid or counter-offer submitted", and the word is load-bearing: Docs/02 §2 has no
# `Negotiating → Negotiating` row and the guarded transition refuses one, so a platform moving the job
# per bid would fail on the second offer rather than ignore it.
after="$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status || ' ' || (select count(*) from bids where job_id = j.id)::text || ' '
          || (select count(*) from job_status_history where job_id = j.id)::text
     from jobs j where j.id = '$second_job';")"
[[ "$after" == "Negotiating 2 2" ]] \
  || fail "the job reads '$after' (status, bids, history rows), want 'Negotiating 2 2' — two offers, and one move rather than two"
ok "two live offers leave the job at Negotiating with exactly one move recorded, not one per bid"

# ---------------------------------------------------------------------------------------
ticket "SHIP-84  price and timing are required, and every bad field is named at once"

status="$(bid_post "$bid_provider_token" "verify-bid-empty-$$" "/v1/jobs/$second_job/bids" '{}' empty)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/bid-empty.json"; fail "an empty offer returned $status, want 422"; }
for field in amount_cents pickup_at deliver_by; do
  grep -q "\"$field\"" "$WORKDIR/bid-empty.json" || fail "no detail names $field"
done
ok "an offer with no price and no timing names all three fields at once"

status="$(bid_post "$bid_provider_token" "verify-bid-backwards-$$" "/v1/jobs/$second_job/bids" \
  "{\"amount_cents\":45000,\"pickup_at\":\"$bid_deliver\",\"deliver_by\":\"$bid_pickup\"}" backwards)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/bid-backwards.json"; fail "delivering before collecting returned $status, want 422"; }
grep -q 'deliver_by' "$WORKDIR/bid-backwards.json" || fail "the refusal does not name deliver_by"
ok "delivery before collection is refused, naming the field"

status="$(bid_post "$bid_provider_token" "verify-bid-past-$$" "/v1/jobs/$second_job/bids" \
  '{"amount_cents":45000,"pickup_at":"2020-01-01T09:00:00Z","deliver_by":"2020-01-02T09:00:00Z"}' past)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/bid-past.json"; fail "a collection time in the past returned $status, want 422"; }
ok "and so is a collection time that has already passed"

status="$(bid_post "$bid_provider_token" "verify-bid-status-$$" "/v1/jobs/$second_job/bids" \
  "{\"amount_cents\":45000,\"pickup_at\":\"$bid_pickup\",\"deliver_by\":\"$bid_deliver\",\"status\":\"accepted\"}" setstatus)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-setstatus.json"; fail "a body naming a status returned $status, want 400"; }
grep -q 'status' "$WORKDIR/bid-setstatus.json" || fail "the refusal does not name the field"
ok "a bid's status is not a settable field; the unknown field is reported, not ignored"

# ---------------------------------------------------------------------------------------
ticket "SHIP-85  PATCH /v1/jobs/{id}/bids/{bid_id} — a provider revises their own active bid"

# $retry_job carries this provider's offer from the retry section above, and $bid_job_id was cancelled
# by the disclosure section, so a fresh job is published for the two tickets below to work against.
status="$(bid_post "$bid_customer_token" "verify-bid-job4-$$" /v1/jobs \
  '{"pickup":{"line":"88 Bridge Road","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"7 Little Malop Street","suburb":"Geelong","state":"VIC","postcode":"3220"},
    "goods_description":"Flat-packed shelving","weight_kg":60,"budget_cents":432199}' job4)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-job4.json"; fail "creating the revision job returned $status"; }
revise_job="$(json "$WORKDIR/bid-job4.json" '["id"]')"
move_job "$revise_job" Draft Open

status="$(bid_post "$bid_provider_token" "verify-bid-revbase-$$" "/v1/jobs/$revise_job/bids" "$bid_body" revbase)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-revbase.json"; fail "placing the offer to revise returned $status"; }
revise_bid="$(json "$WORKDIR/bid-revbase.json" '["id"]')"

# bid_patch <token> <key> <path> <body> <name> — the same shape as bid_post, for SHIP-85's verb.
bid_patch() {
  curl -s -X PATCH -o "$WORKDIR/bid-$5.json" -D "$WORKDIR/bid-$5.headers" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$4" \
    "http://localhost:$VERIFY_PORT$3"
}

status="$(curl -s -X PATCH -o "$WORKDIR/bid-rev-anon.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-bid-rev-anon-$$" -H 'Content-Type: application/json' \
  -d '{"amount_cents":39900}' "http://localhost:$VERIFY_PORT/v1/jobs/$revise_job/bids/$revise_bid")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/bid-rev-anon.json"; fail "an unauthenticated revision returned $status, want 401"; }

status="$(curl -s -X PATCH -o "$WORKDIR/bid-rev-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $bid_provider_token" -H 'Content-Type: application/json' \
  -d '{"amount_cents":39900}' "http://localhost:$VERIFY_PORT/v1/jobs/$revise_job/bids/$revise_bid")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-rev-nokey.json"; fail "a revision with no Idempotency-Key returned $status, want 400"; }
ok "it needs a credential and an Idempotency-Key, like every other state-changing route"

status="$(bid_patch "$bid_provider_token" "verify-bid-revise-$$" "/v1/jobs/$revise_job/bids/$revise_bid" \
  '{"amount_cents":39900,"message":"Two people and a tail lift."}' revise)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-revise.json"; fail "revising returned $status, want 200"; }
[[ "$(json "$WORKDIR/bid-revise.json" '["amount_cents"]')" == "39900" ]] || fail "the revised price did not come back"
[[ "$(json "$WORKDIR/bid-revise.json" '["id"]')" == "$revise_bid" ]] \
  || fail "the revision answered with a different bid — a revision is not a counter-offer"
[[ "$(json "$WORKDIR/bid-revise.json" '["status"]')" == "submitted" ]] \
  || fail "the revised offer is $(json "$WORKDIR/bid-revise.json" '["status"]'), want submitted"
ok "a provider revises their own active bid, and it stays the same live offer at a new number"

# The row. `idempotency_key` is the one worth reading: it holds the key the offer was PLACED under,
# and a revision that overwrote it would leave a late placement retry meeting
# uq_bids_one_submitted_per_provider_per_job instead of its own row — a 409 for a request that
# succeeded. The count is the other half: a revision writes no second row.
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select status || ' ' || amount::text || ' ' || idempotency_key
     from bids where id = '$revise_bid';")"
[[ "$stored" == "Submitted 399.00 verify-bid-revbase-$$" ]] \
  || fail "the revised row is '$stored', want 'Submitted 399.00 verify-bid-revbase-$$' — the placement's key must survive"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids where provider_id = '$bid_provider_id' and job_id = '$revise_job';")" == "1" ]] \
  || fail "the revision wrote a second row"
ok "the row holds the new price, the same status, and still the key the offer was placed under"

# Only what was named. The timing was not sent above, so it must be exactly what was placed.
[[ "$(json "$WORKDIR/bid-revise.json" '["pickup_at"]')" == "$(json "$WORKDIR/bid-revbase.json" '["pickup_at"]')" ]] \
  || fail "pickup_at moved without being named"
[[ "$(json "$WORKDIR/bid-revise.json" '["deliver_by"]')" == "$(json "$WORKDIR/bid-revbase.json" '["deliver_by"]')" ]] \
  || fail "deliver_by moved without being named"
ok "a revision changes only the fields it names"

python3 - "$WORKDIR/bid-revise.json" <<'PY' || fail "the revision response carries something of the customer's"
import json, re, sys

# The same closed set the placement is held to, asserted again because this is a second code path
# answering with the shape — which is exactly where a field safe on one path and not the other lands.
allowed = {
    "id", "job_id", "status", "offered_by", "amount_cents",
    "pickup_at", "deliver_by", "message", "superseded_by",
    "created_at", "updated_at",
    # The collection envelope of Docs/10 §4.5, which the history endpoint answers with (SHIP-88).
    "data", "next_cursor", "has_more",
}

def keys(node):
    if isinstance(node, dict):
        for key, child in node.items():
            yield key
            yield from keys(child)
    elif isinstance(node, list):
        for child in node:
            yield from keys(child)

raw = open(sys.argv[1]).read()
unexpected = sorted(set(keys(json.loads(raw))) - allowed)
if unexpected:
    print(sys.argv[1], "carries keys this API never promised a provider:", unexpected, file=sys.stderr)
    sys.exit(1)
if "budget" in raw.lower():
    print(sys.argv[1], "mentions the budget:", raw, file=sys.stderr)
    sys.exit(1)

identifier = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")
searchable = identifier.sub("<id>", raw)
for rendering in ("4321.99", "432199", "4,321.99"):
    if rendering in searchable:
        print(sys.argv[1], "carries the budget's value as", rendering, file=sys.stderr)
        sys.exit(1)
PY
ok "the revision response is the same closed set of keys, and carries nothing of the customer's"

status="$(bid_patch "$bid_provider_token" "verify-bid-revempty-$$" "/v1/jobs/$revise_job/bids/$revise_bid" '{}' revempty)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-revempty.json"; fail "a revision naming no field returned $status, want 400"; }

status="$(bid_patch "$bid_provider_token" "verify-bid-revstatus-$$" "/v1/jobs/$revise_job/bids/$revise_bid" \
  '{"status":"withdrawn"}' revstatus)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-revstatus.json"; fail "a revision naming a status returned $status, want 400"; }
ok "a revision that names nothing is refused, and a bid's status is still not a settable field"

status="$(bid_patch "$bid_provider_token" "verify-bid-revbad-$$" "/v1/jobs/$revise_job/bids/$revise_bid" \
  "{\"amount_cents\":45000,\"deliver_by\":\"2020-01-01T09:00:00Z\"}" revbad)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/bid-revbad.json"; fail "delivery before collection returned $status, want 422"; }
grep -q 'deliver_by' "$WORKDIR/bid-revbad.json" || fail "the refusal does not name deliver_by"
ok "the merged offer meets the same validator a placement meets, naming the field"

# ---------------------------------------------------------------------------------------
ticket "SHIP-85  \"their own\" is the whole of the authorisation, and a refusal discloses nothing"

status="$(bid_patch "$bid_rival_token" "verify-bid-revrival-$$" "/v1/jobs/$revise_job/bids/$revise_bid" \
  '{"amount_cents":1}' revrival)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-revrival.json"; fail "a competitor revised somebody else's bid: $status"; }

status="$(bid_patch "$bid_rival_token" "verify-bid-revnothing-$$" \
  "/v1/jobs/$revise_job/bids/00000000-0000-7000-8000-000000000085" '{"amount_cents":1}' revnothing)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-revnothing.json"; fail "a bid that does not exist returned $status"; }

python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$WORKDIR/bid-revrival.json" "$WORKDIR/bid-revnothing.json" \
  || fail "somebody else's bid answers differently from one that does not exist"
ok "another provider's bid answers exactly what a bid that is not there answers"

# The bid is real and the caller owns it; only the job it is paired with is wrong. Without that
# comparison the first half of the URL would be decorative.
status="$(bid_patch "$bid_provider_token" "verify-bid-revwrongjob-$$" "/v1/jobs/$retry_job/bids/$revise_bid" \
  '{"amount_cents":1}' revwrongjob)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-revwrongjob.json"; fail "a bid was reachable under the wrong job: $status"; }
ok "and a bid paired with the wrong job is not found either — one resource, one address"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select amount::text from bids where id = '$revise_bid';")" == "399.00" ]] \
  || fail "a refused revision changed the row"
ok "and none of the refusals touched the offer"

# ---------------------------------------------------------------------------------------
ticket "SHIP-86  POST /v1/jobs/{id}/bids/{bid_id}/withdraw — withdrawn before acceptance"

withdraw_path="/v1/jobs/$revise_job/bids/$revise_bid/withdraw"

status="$(curl -s -X POST -o "$WORKDIR/bid-wd-anon.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-bid-wd-anon-$$" -H 'Content-Type: application/json' -d '{}' \
  "http://localhost:$VERIFY_PORT$withdraw_path")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/bid-wd-anon.json"; fail "an unauthenticated withdrawal returned $status, want 401"; }

status="$(curl -s -X POST -o "$WORKDIR/bid-wd-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $bid_provider_token" -H 'Content-Type: application/json' -d '{}' \
  "http://localhost:$VERIFY_PORT$withdraw_path")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-wd-nokey.json"; fail "a withdrawal with no Idempotency-Key returned $status, want 400"; }

status="$(bid_post "$bid_rival_token" "verify-bid-wdrival-$$" "$withdraw_path" '{}' wdrival)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-wdrival.json"; fail "a competitor withdrew somebody else's bid: $status"; }
ok "it needs a credential and a key, and another provider cannot reach the bid at all"

status="$(bid_post "$bid_provider_token" "verify-bid-withdraw-$$" "$withdraw_path" '{}' withdraw)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-withdraw.json"; fail "withdrawing returned $status, want 200"; }
[[ "$(json "$WORKDIR/bid-withdraw.json" '["status"]')" == "withdrawn" ]] \
  || fail "the offer came back as $(json "$WORKDIR/bid-withdraw.json" '["status"]'), want withdrawn"
[[ "$(json "$WORKDIR/bid-withdraw.json" '["id"]')" == "$revise_bid" ]] || fail "the withdrawal names a different bid"
ok "a provider withdraws their own offer before acceptance, and the status becomes Withdrawn"

# The row survives with its price. Docs/01 §4.3 requires every withdrawal to be recorded and Docs/02
# §4 keeps the history readable — there is no delete on this table.
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select status || ' ' || amount::text from bids where id = '$revise_bid';")"
[[ "$stored" == "Withdrawn 399.00" ]] \
  || fail "the stored bid is '$stored', want 'Withdrawn 399.00' — a withdrawal is not a delete"
ok "the row survives as record, at Withdrawn, with the price it was withdrawn at"

python3 - "$WORKDIR/bid-withdraw.json" <<'PY' || fail "the withdrawal response carries something of the customer's"
import json, re, sys

allowed = {
    "id", "job_id", "status", "offered_by", "amount_cents",
    "pickup_at", "deliver_by", "message", "superseded_by",
    "created_at", "updated_at",
    # The collection envelope of Docs/10 §4.5, which the history endpoint answers with (SHIP-88).
    "data", "next_cursor", "has_more",
}

def keys(node):
    if isinstance(node, dict):
        for key, child in node.items():
            yield key
            yield from keys(child)
    elif isinstance(node, list):
        for child in node:
            yield from keys(child)

raw = open(sys.argv[1]).read()
unexpected = sorted(set(keys(json.loads(raw))) - allowed)
if unexpected:
    print(sys.argv[1], "carries keys this API never promised a provider:", unexpected, file=sys.stderr)
    sys.exit(1)
if "budget" in raw.lower():
    print(sys.argv[1], "mentions the budget:", raw, file=sys.stderr)
    sys.exit(1)

identifier = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")
searchable = identifier.sub("<id>", raw)
for rendering in ("4321.99", "432199", "4,321.99"):
    if rendering in searchable:
        print(sys.argv[1], "carries the budget's value as", rendering, file=sys.stderr)
        sys.exit(1)
PY
ok "the withdrawal response is the same closed set of keys, and carries nothing of the customer's"

# A revision after a withdrawal is refused, which is the code a client acts on rather than reports.
status="$(bid_patch "$bid_provider_token" "verify-bid-revclosed-$$" "/v1/jobs/$revise_job/bids/$revise_bid" \
  '{"amount_cents":1}' revclosed)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-revclosed.json"; fail "revising a withdrawn offer returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-revclosed.json" '["error"]["code"]')" == "bidding_bid_closed" ]] \
  || { cat "$WORKDIR/bid-revclosed.json"; fail "expected code=bidding_bid_closed"; }
ok "a withdrawn offer can no longer be revised, with a code the app can act on"

# ---------------------------------------------------------------------------------------
ticket "SHIP-86  a retry of a withdrawal is not a failure, whichever key it carries"

# **Two mechanisms again, and this section tells them apart the way the placement's does.** The
# middleware replays a response while its Redis entry lives. Delete the entry and the request runs all
# the way to the row — where, unlike a placement, there is no stored key to match it. What answers is
# the state: the caller asked for an outcome that already holds.
status="$(bid_post "$bid_provider_token" "verify-bid-withdraw-$$" "$withdraw_path" '{}' wd2)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-wd2.json"; fail "the cached retry returned $status, want the stored 200"; }
replayed_from_redis wd2 || fail "the second withdrawal was not replayed by the middleware"
diff -q "$WORKDIR/bid-withdraw.json" "$WORKDIR/bid-wd2.json" >/dev/null \
  || fail "the replayed response is not byte-identical to the original"
ok "a retry inside the cache is replayed by the middleware, byte for byte"

forget_the_cached_response "$bid_provider_id" "verify-bid-withdraw-$$"
before_updated="$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$revise_bid';")"

status="$(bid_post "$bid_provider_token" "verify-bid-withdraw-$$" "$withdraw_path" '{}' wd3)"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/bid-wd3.json"; fail "a withdrawal retried after the cache forgot it returned $status, want 200"; }
replayed_from_redis wd3 && fail "the third request was still answered from the cache"
ok "and a retry the cache has forgotten is still 200 — answered by the state, not by a stored key"

# A *fresh* key for the same intent, which is what a phone that restarted actually sends. This is the
# case no idempotency mechanism can absorb, and the reason withdrawing twice has to succeed.
status="$(bid_post "$bid_provider_token" "verify-bid-withdraw-again-$$" "$withdraw_path" '{}' wd4)"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/bid-wd4.json"; fail "a withdrawal under a FRESH key returned $status, want 200 — a retry must not fail because the first one succeeded"; }
[[ "$(json "$WORKDIR/bid-wd4.json" '["status"]')" == "withdrawn" ]] || fail "the repeat answered with the wrong status"
ok "a repeat under a brand new key succeeds too — the outcome the caller asked for holds"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$revise_bid';")" == "$before_updated" ]] \
  || fail "an absorbed withdrawal wrote to the row"
ok "and none of the repeats wrote to the row"

# ---------------------------------------------------------------------------------------
ticket "SHIP-86  a withdrawn offer frees the provider to bid again, and moves no job"

status="$(bid_post "$bid_provider_token" "verify-bid-replace-$$" "/v1/jobs/$revise_job/bids" "$bid_body" replace)"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/bid-replace.json"; fail "bidding again after withdrawing returned $status, want 201"; }
[[ "$(json "$WORKDIR/bid-replace.json" '["id"]')" != "$revise_bid" ]] || fail "the replacement is not a new row"
ok "a provider who withdrew may place a new offer — a fat-fingered price is not a job lost forever"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids where provider_id = '$bid_provider_id' and job_id = '$revise_job';")" == "2" ]] \
  || fail "the withdrawn offer did not survive alongside the replacement"
ok "and both rows stand — the withdrawn one as record, the new one as the live offer"

# ---------------------------------------------------------------------------------------
ticket "SHIP-90  the last offer leaving returns the job to Open, and the next one moves it back"

# Docs/02 §2's second presentation row: `Negotiating → Open` on "all active bids expire, are
# withdrawn, or are rejected". $revise_job has now been round the whole loop through real endpoints
# and nothing else — published, offered on, revised, withdrawn, offered on again — so its history is
# the round trip written down.
#
# **Read as the ordered list rather than as a count**, because a count would pass on a job that
# moved to Negotiating twice and never came back. A revision is deliberately absent from it: it
# neither adds a live offer nor removes one, so the job's standing cannot have changed.
round_trip="$("$PSQL" "$DATABASE_URL" -tAc \
  "select string_agg(from_status || '->' || to_status, ' ' order by actor_recorded_at, id)
     from job_status_history where job_id = '$revise_job';")"
[[ "$round_trip" == "Draft->Open Open->Negotiating Negotiating->Open Open->Negotiating" ]] \
  || fail "the job's history is '$round_trip', want 'Draft->Open Open->Negotiating Negotiating->Open Open->Negotiating'"
ok "an offer moves the job to Negotiating, withdrawing the last one returns it to Open, and the next moves it back"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history
     where job_id = '$revise_job' and to_status = 'Open' and from_status = 'Negotiating'
       and actor_type = 'system' and actor_id is null and reason is not null;")" == "1" ]] \
  || fail "the return to Open was not recorded as the platform with a reason"
ok "and the return is the platform's too — nobody asked for it, the last live offer simply left"

# ---------------------------------------------------------------------------------------
ticket "SHIP-87  POST /v1/jobs/{id}/bids/{bid_id}/counter — either party answers the other's offer"

# # The negotiation these checks build, and why it is one fixture rather than several
#
# A chain only means anything after several rounds, and each round's refusals are about the state the
# round before it left. So the checks below run against one negotiation that grows as they go, and the
# comments say which round each one is standing on. `$revise_job` is not reused: it carries a
# withdrawn offer and a replacement from SHIP-86's checks, and "exactly this many rows" would then be
# an assertion about another ticket's fixture.
status="$(bid_post "$bid_customer_token" "verify-bid-job5-$$" /v1/jobs \
  '{"pickup":{"line":"41 Bridge Road","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"90 Moorabool Street","suburb":"Geelong","state":"VIC","postcode":"3220"},
    "goods_description":"Office chairs, six","weight_kg":90,"budget_cents":432199}' job5)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-job5.json"; fail "creating the counter job returned $status"; }
counter_job="$(json "$WORKDIR/bid-job5.json" '["id"]')"
move_job "$counter_job" Draft Open

# bid_get <token> <path> <name> — one authenticated read, for SHIP-88's history.
bid_get() {
  curl -s -o "$WORKDIR/bid-$3.json" -D "$WORKDIR/bid-$3.headers" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" "http://localhost:$VERIFY_PORT$2"
}

# counter_at <token> <key> <bid> <cents> <name> — one round of the negotiation.
counter_at() {
  bid_post "$1" "$2" "/v1/jobs/$counter_job/bids/$3/counter" "{\"amount_cents\":$4}" "$5"
}

# Round 1. The provider's opening offer, at 45000 cents.
status="$(bid_post "$bid_provider_token" "verify-bid-r1-$$" "/v1/jobs/$counter_job/bids" "$bid_body" r1c)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-r1c.json"; fail "placing the offer to counter returned $status"; }
round1="$(json "$WORKDIR/bid-r1c.json" '["id"]')"

counter_path="/v1/jobs/$counter_job/bids/$round1/counter"

status="$(curl -s -X POST -o "$WORKDIR/bid-ct-anon.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-bid-ct-anon-$$" -H 'Content-Type: application/json' \
  -d '{"amount_cents":40000}' "http://localhost:$VERIFY_PORT$counter_path")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/bid-ct-anon.json"; fail "an unauthenticated counter returned $status, want 401"; }

status="$(curl -s -X POST -o "$WORKDIR/bid-ct-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $bid_customer_token" -H 'Content-Type: application/json' \
  -d '{"amount_cents":40000}' "http://localhost:$VERIFY_PORT$counter_path")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-ct-nokey.json"; fail "a counter with no Idempotency-Key returned $status, want 400"; }
ok "it needs a credential and an Idempotency-Key — a counter writes a row, so the key is a column"

# Round 2. **The first request in this file a customer may make.** Every endpoint before it is the
# provider's alone, and this customer's token claims `role: customer` exactly as the provider's does —
# the platform tells them apart by reading jobs.customer_id and bids.provider_id, never by the claim.
status="$(counter_at "$bid_customer_token" "verify-bid-r2-$$" "$round1" 40000 r2c)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-r2c.json"; fail "the customer's counter returned $status, want 201"; }
round2="$(json "$WORKDIR/bid-r2c.json" '["id"]')"
[[ "$round2" != "$round1" ]] || fail "the counter answered with the offer it superseded — a counter creates a row"
[[ "$(json "$WORKDIR/bid-r2c.json" '["offered_by"]')" == "customer" ]] \
  || fail "the counter is offered_by $(json "$WORKDIR/bid-r2c.json" '["offered_by"]'), want customer"
[[ "$(json "$WORKDIR/bid-r2c.json" '["status"]')" == "submitted" ]] || fail "the counter is not live"
[[ "$(json "$WORKDIR/bid-r2c.json" '["amount_cents"]')" == "40000" ]] || fail "the counter's price did not come back"
ok "the job's own customer counters a provider's offer — the first endpoint in this domain a customer may call"

# Countering on price alone inherits the timing, which is the decision 000501 deferred to SHIP-87.
[[ "$(json "$WORKDIR/bid-r2c.json" '["pickup_at"]')" == "$(json "$WORKDIR/bid-r1c.json" '["pickup_at"]')" ]] \
  || fail "the counter did not inherit pickup_at from the offer it answers"
[[ "$(json "$WORKDIR/bid-r2c.json" '["deliver_by"]')" == "$(json "$WORKDIR/bid-r1c.json" '["deliver_by"]')" ]] \
  || fail "the counter did not inherit deliver_by"
ok "a counter on price alone inherits the timing it does not restate"

# The rows, not the answer the endpoint gave about itself. This is the whole of "each counter
# supersedes the prior offer".
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select status || ' ' || offered_by || ' ' || coalesce(superseded_by::text, 'none')
     from bids where id = '$round1';")"
[[ "$stored" == "Superseded provider $round2" ]] \
  || fail "the answered offer reads '$stored', want 'Superseded provider $round2'"
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select status || ' ' || offered_by || ' ' || amount::text || ' ' || coalesce(superseded_by::text, 'none')
     from bids where id = '$round2';")"
[[ "$stored" == "Submitted customer 400.00 none" ]] \
  || fail "the counter row reads '$stored', want 'Submitted customer 400.00 none'"
ok "the answered offer is Superseded and linked to the counter; the counter is the live head with no successor"

# One live offer in the negotiation, which is what uq_bids_one_submitted_per_provider_per_job means
# once a chain exists — and the property SHIP-92 reads under its lock.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids where job_id = '$counter_job' and provider_id = '$bid_provider_id'
     and status = 'Submitted';")" == "1" ]] \
  || fail "the negotiation holds more than one live offer"
ok "exactly one live offer in the negotiation — the head of the chain and the live row are one row"

python3 - "$WORKDIR/bid-r2c.json" <<'PY' || fail "the counter response carries something of the customer's"
import json, re, sys

# **The hardest case for Docs/01 §4.3 in this domain, and the reason this block is repeated here.**
# A counter's amount is a number the *customer* chose, in a response a provider reads back out of the
# history. It is not the budget — a counter-offer is an offer the customer deliberately made — and the
# way that stays true is the same closed key set, applied to the new shape.
allowed = {
    "id", "job_id", "status", "offered_by", "amount_cents",
    "pickup_at", "deliver_by", "message", "superseded_by",
    "created_at", "updated_at",
    "data", "next_cursor", "has_more",
}

def keys(node):
    if isinstance(node, dict):
        for key, child in node.items():
            yield key
            yield from keys(child)
    elif isinstance(node, list):
        for child in node:
            yield from keys(child)

raw = open(sys.argv[1]).read()
unexpected = sorted(set(keys(json.loads(raw))) - allowed)
if unexpected:
    print(sys.argv[1], "carries keys this API never promised:", unexpected, file=sys.stderr)
    sys.exit(1)
if "budget" in raw.lower():
    print(sys.argv[1], "mentions the budget:", raw, file=sys.stderr)
    sys.exit(1)

identifier = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")
searchable = identifier.sub("<id>", raw)
for rendering in ("4321.99", "432199", "4,321.99"):
    if rendering in searchable:
        print(sys.argv[1], "carries the budget's value as", rendering, file=sys.stderr)
        sys.exit(1)
PY
ok "the counter response is the same closed set of keys, and the customer's budget is in none of them"

# Round 3. The other direction. Docs/02 §4's two sentences are one act, so this is the same endpoint
# with the parties the other way round.
status="$(counter_at "$bid_provider_token" "verify-bid-r3-$$" "$round2" 43000 r3c)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-r3c.json"; fail "the provider's counter returned $status, want 201"; }
round3="$(json "$WORKDIR/bid-r3c.json" '["id"]')"
[[ "$(json "$WORKDIR/bid-r3c.json" '["offered_by"]')" == "provider" ]] \
  || fail "the provider's counter is attributed to the wrong party"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from bids where id = '$round2';")" == "Superseded" ]] \
  || fail "the customer's counter was not superseded by the provider's answer"
ok "the provider counters back, and the customer's offer is superseded in turn — one endpoint, both directions"

# ---------------------------------------------------------------------------------------
ticket "SHIP-87  you counter the other party's offer and revise your own"

# Round 3 is the live head and the provider made it, so this is the provider answering themselves.
status="$(counter_at "$bid_provider_token" "verify-bid-ctown-$$" "$round3" 44000 ctown)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-ctown.json"; fail "a provider countered their own offer: $status"; }
[[ "$(json "$WORKDIR/bid-ctown.json" '["error"]["code"]')" == "bidding_wrong_party" ]] \
  || { cat "$WORKDIR/bid-ctown.json"; fail "expected code=bidding_wrong_party"; }
ok "countering an offer you made yourself is refused with a code that tells the client to revise instead"

# Round 1 was superseded two rounds ago. Only the latest valid offer can be acted on.
status="$(counter_at "$bid_customer_token" "verify-bid-stale-$$" "$round1" 38000 stale)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-stale.json"; fail "a superseded offer was countered: $status"; }
[[ "$(json "$WORKDIR/bid-stale.json" '["error"]["code"]')" == "bidding_bid_closed" ]] \
  || { cat "$WORKDIR/bid-stale.json"; fail "expected code=bidding_bid_closed"; }
ok "only the latest valid offer can be countered — a superseded one is closed"

status="$(bid_post "$bid_customer_token" "verify-bid-ctempty-$$" \
  "/v1/jobs/$counter_job/bids/$round3/counter" '{}' ctempty)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-ctempty.json"; fail "a counter naming no field returned $status, want 400"; }

status="$(bid_post "$bid_customer_token" "verify-bid-ctparty-$$" \
  "/v1/jobs/$counter_job/bids/$round3/counter" '{"amount_cents":40000,"offered_by":"provider"}' ctparty)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-ctparty.json"; fail "a body naming offered_by returned $status, want 400"; }
grep -q 'offered_by' "$WORKDIR/bid-ctparty.json" || fail "the refusal does not name offered_by"
ok "a counter that changes nothing is refused, and which party made an offer is not a settable field"

# Round 4, so that the *other* party's offer is the live one for the two checks below.
status="$(counter_at "$bid_customer_token" "verify-bid-r4-$$" "$round3" 41500 r4c)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-r4c.json"; fail "the customer's second counter returned $status"; }
round4="$(json "$WORKDIR/bid-r4c.json" '["id"]')"

# The hole 000502's reinterpretation of provider_id would otherwise have opened: a customer's counter
# carries the provider's id, so without the authorship check the provider could rewrite the customer's
# own number, or withdraw it.
status="$(bid_patch "$bid_provider_token" "verify-bid-crev-$$" \
  "/v1/jobs/$counter_job/bids/$round4" '{"amount_cents":1}' crev)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-crev.json"; fail "the provider revised the customer's counter: $status"; }
[[ "$(json "$WORKDIR/bid-crev.json" '["error"]["code"]')" == "bidding_wrong_party" ]] \
  || { cat "$WORKDIR/bid-crev.json"; fail "expected code=bidding_wrong_party on the revision"; }

status="$(bid_post "$bid_provider_token" "verify-bid-cwd-$$" \
  "/v1/jobs/$counter_job/bids/$round4/withdraw" '{}' cwd)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-cwd.json"; fail "the provider withdrew the customer's counter: $status"; }
[[ "$(json "$WORKDIR/bid-cwd.json" '["error"]["code"]')" == "bidding_wrong_party" ]] \
  || { cat "$WORKDIR/bid-cwd.json"; fail "expected code=bidding_wrong_party on the withdrawal"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status || ' ' || amount::text from bids where id = '$round4';")" \
   == "Submitted 415.00" ]] \
  || fail "the customer's counter was changed by the provider"
ok "and neither party can revise or withdraw an offer the other one made"

status="$(counter_at "$bid_customer_token" "verify-bid-ctown2-$$" "$round4" 39000 ctown2)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-ctown2.json"; fail "a customer countered their own counter: $status"; }
[[ "$(json "$WORKDIR/bid-ctown2.json" '["error"]["code"]')" == "bidding_wrong_party" ]] \
  || { cat "$WORKDIR/bid-ctown2.json"; fail "expected code=bidding_wrong_party"; }
ok "the rule holds from the customer's side too — one endpoint, one rule, both parties"

# ---------------------------------------------------------------------------------------
ticket "SHIP-87  a counter from anybody who is not a party answers what a missing bid answers"

status="$(counter_at "$bid_rival_token" "verify-bid-ctrival-$$" "$round4" 1 ctrival)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-ctrival.json"; fail "a competing provider countered: $status"; }

status="$(bid_post "$bid_rival_token" "verify-bid-ctnothing-$$" \
  "/v1/jobs/$counter_job/bids/00000000-0000-7000-8000-000000000087/counter" '{"amount_cents":1}' ctnothing)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-ctnothing.json"; fail "a counter on nothing returned $status"; }

python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$WORKDIR/bid-ctrival.json" "$WORKDIR/bid-ctnothing.json" \
  || fail "somebody else's negotiation answers differently from one that does not exist"
ok "a competitor's counter answers exactly what a bid that is not there answers — the refusal confirms nothing"

# ---------------------------------------------------------------------------------------
ticket "SHIP-88  only the latest valid offer is acceptable, and the database is what says so"

# **This is what SHIP-92 is being handed.** Docs/11 §8 warns that the award's lock ordering has to be
# designed against uq_bids_one_accepted_per_job rather than around it; these two constraints are the
# other half, and they hold whatever order the award takes its locks in. The statements are
# deliberately the naive ones — no lock, no status check — because that is what a plausible first
# draft would write.
refusal="$("$PSQL" "$DATABASE_URL" -tAc \
  "update bids set status = 'Accepted' where id = '$round1';" 2>&1 || true)"
grep -q 'ck_bids_superseded_is_not_live' <<<"$refusal" \
  || fail "a superseded offer was awarded, or refused by something else: $refusal"
ok "a superseded offer cannot be awarded — ck_bids_superseded_is_not_live, not a rule SHIP-92 has to remember"

# Round 4 is the live head and the *customer* made it. Awarding it would bind the provider to a price
# and a date they never offered.
refusal="$("$PSQL" "$DATABASE_URL" -tAc \
  "update bids set status = 'Accepted' where id = '$round4';" 2>&1 || true)"
grep -q 'ck_bids_only_a_providers_offer_is_accepted' <<<"$refusal" \
  || fail "the customer's own counter was awarded, or refused by something else: $refusal"
ok "and neither can the customer's own counter — Docs/02 §1's \"provider commitment exists\", as a constraint"

# ---------------------------------------------------------------------------------------
ticket "SHIP-87  a retried counter adds no second link to the chain"

# **The two mechanisms again, told apart the way the placement's checks tell them apart.** The key
# matters more here than for a revision: a counter writes a row, so a retry the cache has forgotten
# would add a second link — and the retry has to be answered *before* the status is checked, because
# by then the caller's own first attempt has already superseded the offer it answered.
#
# Round 5, made by the provider, so that the head afterwards is a provider's offer.
before_rows="$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from bids where job_id = '$counter_job';")"

status="$(counter_at "$bid_provider_token" "verify-bid-r5-$$" "$round4" 42000 r5a)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-r5a.json"; fail "the counter returned $status, want 201"; }
replayed_from_redis r5a && fail "the first counter was answered from the cache"
round5="$(json "$WORKDIR/bid-r5a.json" '["id"]')"

status="$(counter_at "$bid_provider_token" "verify-bid-r5-$$" "$round4" 42000 r5b)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-r5b.json"; fail "the cached retry returned $status, want the stored 201"; }
replayed_from_redis r5b || fail "the second request was not replayed by the middleware"
diff -q "$WORKDIR/bid-r5a.json" "$WORKDIR/bid-r5b.json" >/dev/null \
  || fail "the replayed response is not byte-identical to the original"
ok "a retry inside the cache is replayed by the middleware, byte for byte"

forget_the_cached_response "$bid_provider_id" "verify-bid-r5-$$"

status="$(counter_at "$bid_provider_token" "verify-bid-r5-$$" "$round4" 42000 r5c)"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/bid-r5c.json"; fail "a counter retried after the cache forgot it returned $status, want 200 from the row"; }
replayed_from_redis r5c && fail "the third request was still answered from the cache"
[[ "$(json "$WORKDIR/bid-r5c.json" '["id"]')" == "$round5" ]] \
  || fail "the retry answered with a different counter"
ok "and a retry the cache has forgotten is answered from the row — 200, the same counter, not a refusal"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from bids where job_id = '$counter_job';")" \
   == "$((before_rows + 1))" ]] \
  || fail "three requests under one key added more than one link to the chain"
ok "one new link at the end of all three — the key is a column, not only a cache entry"

# The provider's offer at the head of a chain *is* awardable, which is what makes the pair of
# constraints a shape rather than a wall: a customer who wants their own number waits for the provider
# to counter at it. Reverted immediately, because the checks below still need a live negotiation.
"$PSQL" "$DATABASE_URL" -q -c "update bids set status = 'Accepted' where id = '$round5';" \
  || fail "a provider's offer at the head of a chain could not be awarded"
"$PSQL" "$DATABASE_URL" -q -c "update bids set status = 'Submitted' where id = '$round5';"
ok "a provider's counter at the head of the chain is awardable — the customer waits for the offer rather than awarding their own"

# ---------------------------------------------------------------------------------------
ticket "SHIP-90  a counter moves nothing, because the offer it answers already did"

# Docs/02 §2 has `Open → Negotiating` on "first bid or **counter-offer** submitted", and the second
# half of that sentence reads like an instruction to the counter endpoint. It is not one, and the
# reason is "first": a counter answers a *live* offer, and a live offer is one a placement wrote — so
# the job moved when that placement happened and there is nothing left for a counter to do. A counter
# does not end the negotiation either: it supersedes one live offer and inserts another.
#
# Five rounds, and exactly one move: the history count is the half that catches a move made and then
# reversed through some other path.
after="$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status || ' ' || (select count(*) from job_status_history where job_id = j.id)::text
     from jobs j where j.id = '$counter_job';")"
[[ "$after" == "Negotiating 2" ]] \
  || fail "the job reads '$after' (status, history rows), want 'Negotiating 2' — its publication and the one move the first offer made"
ok "a job with five rounds of negotiation on it has moved exactly once, and that was the first offer"

# ---------------------------------------------------------------------------------------
ticket "SHIP-88  GET /v1/jobs/{id}/bids/{bid_id}/history — the full chain stays readable"

# Addressed through the *first* offer, which is the ordinary case: a client holding an old identifier
# still gets the whole exchange.
history_path="/v1/jobs/$counter_job/bids/$round1/history"

status="$(curl -s -o "$WORKDIR/bid-hist-anon.json" -w '%{http_code}' "http://localhost:$VERIFY_PORT$history_path")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/bid-hist-anon.json"; fail "an unauthenticated history read returned $status, want 401"; }

# Read-only, so no Idempotency-Key — the middleware lets safe methods through untouched, and an
# endpoint demanding one would be asking a client to generate a value per read.
status="$(bid_get "$bid_customer_token" "$history_path" histcust)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-histcust.json"; fail "the customer's history read returned $status, want 200"; }
ok "it needs a credential and no Idempotency-Key — nothing changes, so there is nothing to absorb"

status="$(bid_get "$bid_provider_token" "$history_path" histprov)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-histprov.json"; fail "the provider's history read returned $status, want 200"; }
diff -q "$WORKDIR/bid-histcust.json" "$WORKDIR/bid-histprov.json" >/dev/null \
  || fail "the two parties see different histories; Docs/02 §4 gives both the same chain"
ok "both parties read the negotiation, and they read the same thing"

python3 - "$WORKDIR/bid-histprov.json" "$round1" "$round2" "$round3" "$round4" "$round5" \
  <<'PY' || fail "the chain is not readable end to end"
import json, sys

page = json.load(open(sys.argv[1]))
want = sys.argv[2:]

if page["has_more"] or page["next_cursor"] is not None:
    print("the history paged or reported itself truncated:", page, file=sys.stderr)
    sys.exit(1)

got = [entry["id"] for entry in page["data"]]
if got != want:
    print("the chain reads", got, "want", want, "oldest first", file=sys.stderr)
    sys.exit(1)

parties = [entry["offered_by"] for entry in page["data"]]
if parties != ["provider", "customer", "provider", "customer", "provider"]:
    print("the rounds are attributed", parties, file=sys.stderr)
    sys.exit(1)

amounts = [entry["amount_cents"] for entry in page["data"]]
if amounts != [45000, 40000, 43000, 41500, 42000]:
    print("an intermediate price was lost:", amounts, file=sys.stderr)
    sys.exit(1)

for i, entry in enumerate(page["data"][:-1]):
    if entry["status"] != "superseded":
        print("round", i + 1, "is", entry["status"], "want superseded", file=sys.stderr)
        sys.exit(1)
    if entry.get("superseded_by") != page["data"][i + 1]["id"]:
        print("round", i + 1, "points at", entry.get("superseded_by"), file=sys.stderr)
        sys.exit(1)

head = page["data"][-1]
if head["status"] != "submitted" or "superseded_by" in head:
    print("the last round is not the live head:", head, file=sys.stderr)
    sys.exit(1)
PY
ok "five rounds, oldest first, each at the price it was made at, each linked to the one that answered it"

python3 - "$WORKDIR/bid-histprov.json" <<'PY' || fail "the history carries something of the customer's"
import json, re, sys

# **The response where the customer's own numbers reach a provider**, which makes it the sharpest case
# for Docs/01 §4.3 in this domain. The counters are offers the customer deliberately made and are
# meant to be read; the budget is not, and is not here in any form.
allowed = {
    "id", "job_id", "status", "offered_by", "amount_cents",
    "pickup_at", "deliver_by", "message", "superseded_by",
    "created_at", "updated_at",
    "data", "next_cursor", "has_more",
}

def keys(node):
    if isinstance(node, dict):
        for key, child in node.items():
            yield key
            yield from keys(child)
    elif isinstance(node, list):
        for child in node:
            yield from keys(child)

raw = open(sys.argv[1]).read()
unexpected = sorted(set(keys(json.loads(raw))) - allowed)
if unexpected:
    print(sys.argv[1], "carries keys this API never promised a provider:", unexpected, file=sys.stderr)
    sys.exit(1)
if "budget" in raw.lower():
    print(sys.argv[1], "mentions the budget:", raw, file=sys.stderr)
    sys.exit(1)

identifier = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")
searchable = identifier.sub("<id>", raw)
for rendering in ("4321.99", "432199", "4,321.99"):
    if rendering in searchable:
        print(sys.argv[1], "carries the budget's value as", rendering, file=sys.stderr)
        sys.exit(1)
PY
ok "the history is the same closed set of keys at every depth, and carries nothing of the customer's budget"

status="$(bid_get "$bid_rival_token" "$history_path" histrival)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-histrival.json"; fail "a competing provider read the negotiation: $status"; }
ok "a competing provider cannot read it at all — a whole negotiation in one response is exactly what Docs/01 §4.3 keeps private"

# ---------------------------------------------------------------------------------------
ticket "SHIP-96  Docs/02 §4's three readers, and the only answer everybody else gets"

# SHIP-96's *Done when*: "customer, bidding provider, and admin each see only what Docs/02 §4
# permits." That section's last line is the whole rule — "bid history remains visible to the
# customer, bidding provider, and administrators" — and SHIP-88 built the read and served the first
# two, naming this ticket for the third and for the rules in full.
#
# # What is demonstrated here, and the one third that is demonstrated by tests
#
# internal/bidding/visibility_test.go holds all three audiences and every refusal, against a real
# database. **The administrator's third cannot be demonstrated over the wire, and that is a fact
# about the platform rather than a gap in this section**: `authctx.Subject` cannot carry an
# administrator — Docs/06 §5.2 and SHIP-147 make admin sign-in a separate system that a user token
# cannot reach — so there is no credential a client can hold that would make `Viewer.Administrator`
# true. SHIP-147 supplies the session and SHIP-152 the endpoint.
#
# So what this asserts is the half that is served: the two audiences that can reach the route see the
# same negotiation, everybody else gets the 404 a bid that does not exist gets, and the served route
# **cannot build an administrator at all** — which is asserted as the refusal an outsider still gets
# whatever credential they hold.
#
# The competing provider is the case that matters. They hold a real credential, they bid on the same
# job, and the negotiation they are asking for is the one thing Docs/01 §4.3 most needs kept from
# them.

# The two permitted readers see the same rows. Read as the ordered list of identifiers rather than as
# a count, because two audiences seeing different *versions* of one record is the failure this rule
# exists against, and a count would miss it.
for reader in "customer:$bid_customer_token" "provider:$bid_provider_token"; do
  status="$(bid_get "${reader#*:}" "$history_path" "vis-${reader%%:*}")"
  [[ "$status" == "200" ]] \
    || { cat "$WORKDIR/bid-vis-${reader%%:*}.json"; fail "the ${reader%%:*} reading the chain returned $status"; }
done
customer_chain="$(python3 -c '
import json, sys
print(" ".join(row["id"] for row in json.load(open(sys.argv[1]))["data"]))
' "$WORKDIR/bid-vis-customer.json")"
provider_chain="$(python3 -c '
import json, sys
print(" ".join(row["id"] for row in json.load(open(sys.argv[1]))["data"]))
' "$WORKDIR/bid-vis-provider.json")"
[[ -n "$customer_chain" ]] || fail "the customer's read is empty, so this section proves nothing"
[[ "$customer_chain" == "$provider_chain" ]] \
  || fail "the two parties read different chains: [$customer_chain] against [$provider_chain]"
ok "the customer and the bidding provider read the same negotiation, row for row — Docs/02 §4 gives them one record, not two views of it"

# Everybody else, and each of the three is a different kind of caller: a provider bidding on the same
# job, a customer who owns no part of it, and a request with no credential at all.
status="$(bid_get "$bid_rival_token" "$history_path" vis-rival)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-vis-rival.json"; fail "a competing provider read the chain: $status"; }
[[ "$(json "$WORKDIR/bid-vis-rival.json" '["error"]["code"]')" == "not_found" ]] \
  || fail "the competitor's refusal answered $(json "$WORKDIR/bid-vis-rival.json" '["error"]["code"]'), want not_found"

status="$(curl -s -o "$WORKDIR/bid-vis-absent.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $bid_rival_token" \
  "http://localhost:$VERIFY_PORT/v1/jobs/$counter_job/bids/$(uuidgen | tr 'A-Z' 'a-z')/history")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-vis-absent.json"; fail "a bid that does not exist returned $status"; }
diff <(python3 -c 'import json,sys; d=json.load(open(sys.argv[1]))["error"]; print(d["code"], d["message"])' "$WORKDIR/bid-vis-rival.json") \
     <(python3 -c 'import json,sys; d=json.load(open(sys.argv[1]))["error"]; print(d["code"], d["message"])' "$WORKDIR/bid-vis-absent.json") >/dev/null \
  || fail "a negotiation somebody may not read answers differently from one that does not exist"
ok "a competing provider gets the answer a bid that does not exist gets, code and message alike — a refusal that differed would confirm the negotiation"

status="$(bid_get "$bid_customer_token" "/v1/jobs/$counter_job/bids/$(uuidgen | tr 'A-Z' 'a-z')/history" vis-cust-absent)"
[[ "$status" == "404" ]] || fail "an identifier that names nothing returned $status to the job's own customer"
status="$(curl -s -o "$WORKDIR/bid-vis-anon.json" -w '%{http_code}' "http://localhost:$VERIFY_PORT$history_path")"
[[ "$status" == "401" ]] || fail "an unauthenticated read returned $status, want 401"
ok "and a caller with no credential is refused before any of it — the platform decides who is reading, never the device"

unset customer_chain provider_chain reader

# ---------------------------------------------------------------------------------------
ticket "SHIP-88  the record outlives the job, and a chain cannot fork"

move_job "$counter_job" - Cancelled

status="$(bid_get "$bid_customer_token" "$history_path" histover)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-histover.json"; fail "the history on a cancelled job returned $status"; }
[[ "$(python3 -c "import json,sys; print(len(json.load(open(sys.argv[1]))['data']))" "$WORKDIR/bid-histover.json")" == "5" ]] \
  || fail "the chain shrank when the job ended; a record is most useful once the work is over"
ok "both parties still read the whole chain after the job is cancelled — CustomerOf is asked, AwardableBy is not"

# And a counter on it is refused, which is the customer's half of the check a provider meets as
# eligibility: there is nothing a counter-offer could now lead to.
status="$(counter_at "$bid_customer_token" "verify-bid-ctover-$$" "$round5" 39000 ctover)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-ctover.json"; fail "a counter on a cancelled job returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-ctover.json" '["error"]["code"]')" == "bidding_bid_closed" ]] \
  || { cat "$WORKDIR/bid-ctover.json"; fail "expected code=bidding_bid_closed"; }
ok "and a counter on a job that can no longer be awarded is refused — the negotiation is over"

# A chain is a list rather than a tree. "At most one successor per offer" is true by construction, and
# this is the direction the column shape does not give: two offers cannot name one counter as what
# displaced them.
refusal="$("$PSQL" "$DATABASE_URL" -tAc \
  "update bids set superseded_by = '$round3' where id = '$round1';" 2>&1 || true)"
grep -q 'uq_bids_one_successor' <<<"$refusal" \
  || fail "a negotiation merged: two offers were displaced by one counter ($refusal)"
ok "two offers cannot be displaced by the same counter — a chain is a list, not a tree"

# ---------------------------------------------------------------------------------------
ticket "SHIP-92  POST /v1/jobs/{id}/award — one bid accepted and the job moved, in one transaction"

# # A fixture of its own, and three accounts on the 0416x block
#
# The award ends a job, so it cannot run against a negotiation the checks above still need — and the
# rows it leaves behind are what the checks after it assert on. `$award_customer` is a second customer
# deliberately: the award is the first endpoint in this domain where being the *right* customer is the
# whole of the authorisation, and a section with one customer could not tell "the platform read
# jobs.customer_id" from "the platform let any credential through".
status="$(post_json "verify-award-cust-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"award-customer-$$@example.com\",\"phone\":\"04160$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/award-customer.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/award-customer.json"; fail "could not register the awarding customer: $status"; }
award_customer_id="$(json "$WORKDIR/award-customer.json" '["id"]')"
award_customer_token="$(mint_token "$award_customer_id")"

status="$(post_json "verify-award-other-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"award-stranger-$$@example.com\",\"phone\":\"04161$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/award-stranger.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/award-stranger.json"; fail "could not register the stranger: $status"; }
award_stranger_token="$(mint_token "$(json "$WORKDIR/award-stranger.json" '["id"]')")"

# move_job writes `$bid_customer_id` as the actor, so the award job needs its own mover: a history row
# attributing this customer's publication to a different account would be a fixture that lies.
move_award_job() {
  local from="$2"
  if [[ "$from" == "-" ]]; then
    from="$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$1';" | tr -d ' ')"
  fi
  set -- "$1" "$from" "$3"
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 <<SQL
DO \$do\$
DECLARE entry uuid := gen_random_uuid();
BEGIN
  INSERT INTO job_status_history
      (id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
  VALUES (entry, '$1', '$2', '$3', 'customer', '$award_customer_id', now());
  PERFORM set_config('shipper.job_status_transition', entry::text, true);
  UPDATE jobs SET status = '$3' WHERE id = '$1';
END
\$do\$;
SQL
}

# new_award_job <name> — one Open job belonging to the awarding customer, with a budget on it.
new_award_job() {
  local status
  status="$(bid_post "$award_customer_token" "verify-award-job-$1-$$" /v1/jobs \
    '{"pickup":{"line":"7 Coppin Street","suburb":"Richmond","state":"VIC","postcode":"3121"},
      "dropoff":{"line":"55 Little Malop Street","suburb":"Geelong","state":"VIC","postcode":"3220"},
      "goods_description":"Pallet of tiles","weight_kg":300,"budget_cents":432199}' "awjob-$1")"
  [[ "$status" == "201" ]] || { cat "$WORKDIR/bid-awjob-$1.json"; fail "creating award job $1 returned $status"; }
  local id
  id="$(json "$WORKDIR/bid-awjob-$1.json" '["id"]')"
  move_award_job "$id" Draft Open
  printf '%s' "$id"
}

# award <token> <key> <job> <bid> <name> — one award request.
award() {
  bid_post "$1" "$2" "/v1/jobs/$3/award" "{\"bid_id\":\"$4\"}" "$5"
}

award_job="$(new_award_job one)"

status="$(bid_post "$bid_provider_token" "verify-award-offer-$$" "/v1/jobs/$award_job/bids" "$bid_body" awoffer)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-awoffer.json"; fail "the offer to award returned $status"; }
award_bid="$(json "$WORKDIR/bid-awoffer.json" '["id"]')"

# The rival's offer is placed **now**, before the award, and the reason is worth stating rather than
# discovering: once the job is Awarded it is no longer offered to anybody, so a rival asked to bid
# afterwards is refused by the eligibility filter with a 404 — a true answer to a different question,
# and one that would look like a broken fixture. The competing offer the second award below names has
# to exist before the first award closes the job.
status="$(bid_post "$bid_rival_token" "verify-award-rival-$$" "/v1/jobs/$award_job/bids" "$bid_body" awrival)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-awrival.json"; fail "the rival could not bid before the award: $status"; }
rival_award_bid="$(json "$WORKDIR/bid-awrival.json" '["id"]')"

status="$(curl -s -X POST -o "$WORKDIR/bid-aw-anon.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-award-anon-$$" -H 'Content-Type: application/json' \
  -d "{\"bid_id\":\"$award_bid\"}" "http://localhost:$VERIFY_PORT/v1/jobs/$award_job/award")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/bid-aw-anon.json"; fail "an unauthenticated award returned $status, want 401"; }

status="$(curl -s -X POST -o "$WORKDIR/bid-aw-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $award_customer_token" -H 'Content-Type: application/json' \
  -d "{\"bid_id\":\"$award_bid\"}" "http://localhost:$VERIFY_PORT/v1/jobs/$award_job/award")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-aw-nokey.json"; fail "an award with no Idempotency-Key returned $status, want 400"; }
ok "it needs a credential and an Idempotency-Key, like every other state-changing route"

# The refusals come before the success, so that each is asserted against a job that is still awardable
# — a refusal on a job that had already been awarded would pass for the wrong reason.
status="$(award "$bid_provider_token" "verify-award-prov-$$" "$award_job" "$award_bid" awprov)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-awprov.json"; fail "the bidding provider awarded the job: $status"; }
[[ "$(json "$WORKDIR/bid-awprov.json" '["error"]["code"]')" == "not_found" ]] \
  || { cat "$WORKDIR/bid-awprov.json"; fail "expected code=not_found"; }
grep -q "$award_bid" "$WORKDIR/bid-awprov.json" \
  && { cat "$WORKDIR/bid-awprov.json"; fail "the refusal echoes the bid identifier back to a provider"; }

status="$(award "$award_stranger_token" "verify-award-str-$$" "$award_job" "$award_bid" awstr)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-awstr.json"; fail "another customer awarded the job: $status"; }
ok "only the job's own customer may award it — a provider and another customer both get the answer a job that does not exist gets"

status="$(award "$award_customer_token" "verify-award-nobid-$$" "$award_job" "" awnobid)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/bid-awnobid.json"; fail "an award naming no bid returned $status, want 422"; }
[[ "$(json "$WORKDIR/bid-awnobid.json" '["error"]["details"][0]["field"]')" == "bid_id" ]] \
  || { cat "$WORKDIR/bid-awnobid.json"; fail "the refusal does not name bid_id"; }
ok "and the offer it accepts is named in the body, so a missing one is a field error rather than a broken request"

# The award itself.
status="$(award "$award_customer_token" "verify-award-$$" "$award_job" "$award_bid" award)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-award.json"; fail "awarding returned $status, want 200"; }
[[ "$(json "$WORKDIR/bid-award.json" '["id"]')" == "$award_bid" ]] || fail "the award answered with a different bid"
[[ "$(json "$WORKDIR/bid-award.json" '["status"]')" == "accepted" ]] \
  || fail "the bid came back as $(json "$WORKDIR/bid-award.json" '["status"]'), want accepted"
ok "the customer awards one offer and is answered with it, accepted"

# Both tables, in one query, because the *Done when* is one transaction: an accepted bid on a job that
# did not move, or a job at Awarded with nothing accepted, are the two half-states this is here to
# refuse. The history count is the third: 000402 will not let the status move without a row describing
# the move, so 'Awarded 1 3' is a transition that went through the guard rather than around it — three
# being the publication, the move to Negotiating the first offer made (SHIP-90), and the award.
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status
          || ' ' || (select count(*) from bids where job_id = j.id and status = 'Accepted')::text
          || ' ' || (select count(*) from job_status_history where job_id = j.id)::text
     from jobs j where j.id = '$award_job';")"
[[ "$stored" == "Awarded 1 3" ]] \
  || fail "the job reads '$stored' (status, accepted bids, history rows), want 'Awarded 1 3'"
ok "the job is Awarded with exactly one accepted bid and a recorded transition — one act, one transaction"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select actor_type || ' ' || actor_id::text from job_status_history
     where job_id = '$award_job' and to_status = 'Awarded';")" == "customer $award_customer_id" ]] \
  || fail "the transition is not attributed to the customer who made it"
ok "and the transition names the customer as the actor, which is who Docs/02 §3 gives the award to"

python3 - "$WORKDIR/bid-award.json" <<'PY' || fail "the award response carries something of the customer's"
import json, re, sys

# The seventh response in this domain held to the closed key set, and the one where the two sides of a
# negotiation meet — which makes it the shape a later ticket is most tempted to widen.
allowed = {
    "id", "job_id", "status", "offered_by", "amount_cents",
    "pickup_at", "deliver_by", "message", "superseded_by",
    "created_at", "updated_at",
    "data", "next_cursor", "has_more",
}

def keys(node):
    if isinstance(node, dict):
        for key, child in node.items():
            yield key
            yield from keys(child)
    elif isinstance(node, list):
        for child in node:
            yield from keys(child)

raw = open(sys.argv[1]).read()
unexpected = sorted(set(keys(json.loads(raw))) - allowed)
if unexpected:
    print(sys.argv[1], "carries keys this API never promised:", unexpected, file=sys.stderr)
    sys.exit(1)
if "budget" in raw.lower():
    print(sys.argv[1], "mentions the budget:", raw, file=sys.stderr)
    sys.exit(1)

identifier = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")
searchable = identifier.sub("<id>", raw)
for rendering in ("4321.99", "432199", "4,321.99"):
    if rendering in searchable:
        print(sys.argv[1], "carries the budget's value as", rendering, file=sys.stderr)
        sys.exit(1)
PY
ok "the award response is the same closed set of keys, and carries nothing of the customer's budget"

# ---------------------------------------------------------------------------------------
ticket "SHIP-92  an award is idempotent by state, and a job is awarded once"

# The middleware's own replay first, which is a different mechanism from the one below it.
status="$(award "$award_customer_token" "verify-award-$$" "$award_job" "$award_bid" awagain)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-awagain.json"; fail "the cached retry returned $status"; }
replayed_from_redis awagain || fail "the second request was not replayed by the middleware"
diff -q "$WORKDIR/bid-award.json" "$WORKDIR/bid-awagain.json" >/dev/null \
  || fail "the replayed response is not byte-identical to the original"
ok "a retry inside the cache is replayed by the middleware, byte for byte"

# And a **different** key, which is what a restarted phone sends. There is no stored key on this
# endpoint and there is no need for one: an award is an UPDATE of a row that already exists, so
# applying it twice reaches the state applying it once reaches. That is idempotency by state, which
# SHIP-86 established is stronger than a stored key — two clients with two keys still cannot award one
# job twice.
before_updated="$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$award_bid';")"

status="$(award "$award_customer_token" "verify-award-fresh-$$" "$award_job" "$award_bid" awfresh)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-awfresh.json"; fail "an award retried under a fresh key returned $status, want 200"; }
replayed_from_redis awfresh && fail "the request under a fresh key was answered from the cache"
[[ "$(json "$WORKDIR/bid-awfresh.json" '["id"]')" == "$award_bid" ]] || fail "the retry answered with a different bid"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$award_bid';")" == "$before_updated" ]] \
  || fail "the repeated award wrote to the row again"
ok "and one under a fresh key is still 200 with the same bid, having written nothing — idempotent by state, not by key"

# Counted on the award's own row rather than on the whole history, because the job has three
# transitions since SHIP-90 — its publication, the move to Negotiating the offer made, and this — and
# a total would have to be re-derived every time the lifecycle grows a step. What is being asserted
# is that three awards produced one award.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$award_job' and to_status = 'Awarded';")" == "1" ]] \
  || fail "three awards left more than one Awarded transition on the job"
ok "three awards, one transition — the job moved once"

# A **different** offer on the job that has been awarded — the rival's, placed before the award. Since
# SHIP-93 it is `Rejected`, closed by the award itself, and that is what makes the refusal below worth
# checking rather than obvious: an implementation that judged the *offer* before the *job* would
# answer `bidding_bid_closed` here and send the customer to offers the same sweep has just closed.
# What refuses it is the job's own status, held under the lock the award takes first.
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from bids where id = '$rival_award_bid';")" == "Rejected" ]] \
  || fail "the competing offer is not Rejected; the award is supposed to have closed it (SHIP-93)"

status="$(award "$award_customer_token" "verify-award-second-$$" "$award_job" "$rival_award_bid" awsecond)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-awsecond.json"; fail "a second award on one job returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-awsecond.json" '["error"]["code"]')" == "conflict" ]] \
  || { cat "$WORKDIR/bid-awsecond.json"; fail "expected code=conflict, not bidding_bid_closed — the job is what the customer has to look at"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids where job_id = '$award_job' and status = 'Accepted';")" == "1" ]] \
  || fail "a second bid was accepted on one job"
ok "a second award naming an offer the sweep closed is refused with conflict and not bid_closed, and the job still holds exactly one accepted bid"

# The database's own half of the same rule, which is what holds if the ordering above is ever changed.
refusal="$("$PSQL" "$DATABASE_URL" -tAc \
  "update bids set status = 'Accepted' where id = '$rival_award_bid';" 2>&1 || true)"
grep -q 'uq_bids_one_accepted_per_job' <<<"$refusal" \
  || fail "a second bid was accepted at the database level ($refusal)"
ok "and the index refuses it even with the endpoint out of the way — SHIP-91's constraint, not a check that could race"

# ---------------------------------------------------------------------------------------
ticket "SHIP-92  the selected bid must be active, and only a provider's offer can be awarded"

closed_job="$(new_award_job two)"
status="$(bid_post "$bid_provider_token" "verify-award-closed-$$" "/v1/jobs/$closed_job/bids" "$bid_body" awclosed)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-awclosed.json"; fail "the offer to withdraw returned $status"; }
closed_bid="$(json "$WORKDIR/bid-awclosed.json" '["id"]')"

status="$(bid_post "$bid_provider_token" "verify-award-wd-$$" \
  "/v1/jobs/$closed_job/bids/$closed_bid/withdraw" '{}' awwd)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-awwd.json"; fail "withdrawing returned $status"; }

status="$(award "$award_customer_token" "verify-award-wdaward-$$" "$closed_job" "$closed_bid" awwdaward)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-awwdaward.json"; fail "awarding a withdrawn offer returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-awwdaward.json" '["error"]["code"]')" == "bidding_bid_closed" ]] \
  || { cat "$WORKDIR/bid-awwdaward.json"; fail "expected code=bidding_bid_closed"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$closed_job';")" == "Open" ]] \
  || fail "the refused award moved the job"
ok "an offer that is no longer live cannot be awarded, and the refusal points the customer at the other offers rather than at the job"

# The negotiation case, which is the one SHIP-88's chain link exists for: only the head is awardable,
# and the head is only awardable if the *provider* made it.
status="$(bid_post "$bid_provider_token" "verify-award-neg-$$" "/v1/jobs/$closed_job/bids" "$bid_body" awneg)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-awneg.json"; fail "the replacement offer returned $status"; }
neg_round1="$(json "$WORKDIR/bid-awneg.json" '["id"]')"

status="$(bid_post "$award_customer_token" "verify-award-negc-$$" \
  "/v1/jobs/$closed_job/bids/$neg_round1/counter" '{"amount_cents":40000}' awnegc)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-awnegc.json"; fail "the customer's counter returned $status"; }
neg_round2="$(json "$WORKDIR/bid-awnegc.json" '["id"]')"

status="$(award "$award_customer_token" "verify-award-stale-$$" "$closed_job" "$neg_round1" awstale)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-awstale.json"; fail "a superseded offer was awarded: $status"; }
[[ "$(json "$WORKDIR/bid-awstale.json" '["error"]["code"]')" == "bidding_bid_closed" ]] \
  || { cat "$WORKDIR/bid-awstale.json"; fail "expected code=bidding_bid_closed"; }
ok "only the latest offer in a negotiation is acceptable — a displaced one answers the same code a withdrawn one does"

status="$(award "$award_customer_token" "verify-award-own-$$" "$closed_job" "$neg_round2" awown)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-awown.json"; fail "the customer awarded their own counter: $status"; }
[[ "$(json "$WORKDIR/bid-awown.json" '["error"]["code"]')" == "bidding_wrong_party" ]] \
  || { cat "$WORKDIR/bid-awown.json"; fail "expected code=bidding_wrong_party"; }
ok "and a customer cannot award their own counter-offer — awarding it would commit a provider to terms they never agreed to"

# The provider counters back at the customer's number, and *that* row is awardable. This is what makes
# the pair of constraints a shape rather than a wall.
status="$(bid_post "$bid_provider_token" "verify-award-agree-$$" \
  "/v1/jobs/$closed_job/bids/$neg_round2/counter" '{"amount_cents":40000}' awagree)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-awagree.json"; fail "the provider's counter returned $status"; }
neg_round3="$(json "$WORKDIR/bid-awagree.json" '["id"]')"

status="$(award "$award_customer_token" "verify-award-agreed-$$" "$closed_job" "$neg_round3" awagreed)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-awagreed.json"; fail "awarding the provider's counter returned $status, want 200"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status || ' ' || (select amount::text from bids where id = '$neg_round3')
     from jobs j where j.id = '$closed_job';")" == "Awarded 400.00" ]] \
  || fail "the negotiated job did not end at Awarded on the agreed amount"
ok "the provider's counter at the customer's own number is awardable, and the job ends at Awarded on the agreed price"

# ---------------------------------------------------------------------------------------
ticket "SHIP-92  a job that cannot be awarded, and a bid that is not on the job in the path"

gone_job="$(new_award_job three)"
status="$(bid_post "$bid_provider_token" "verify-award-gone-$$" "/v1/jobs/$gone_job/bids" "$bid_body" awgone)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-awgone.json"; fail "the offer on the cancelled job returned $status"; }
gone_bid="$(json "$WORKDIR/bid-awgone.json" '["id"]')"

# The bid on the *first* job, awarded under this one. Both identifiers are real and the pair is not,
# which is the only endpoint in this domain where the two come from two different places.
status="$(award "$award_customer_token" "verify-award-cross-$$" "$gone_job" "$award_bid" awcross)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-awcross.json"; fail "a bid on another job was awarded: $status"; }
# Negotiating rather than Open since SHIP-90: the offer placed on it two statements ago moved it
# there. What is being asserted is that the *refused* award left it wherever the offer had.
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$gone_job';")" == "Negotiating" ]] \
  || fail "the cross-job award moved the job"
ok "a real offer paired with the wrong job names nothing — the job in the path is checked, not decorative"

move_award_job "$gone_job" - Cancelled

status="$(award "$award_customer_token" "verify-award-cancelled-$$" "$gone_job" "$gone_bid" awcancelled)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-awcancelled.json"; fail "a cancelled job was awarded: $status"; }
[[ "$(json "$WORKDIR/bid-awcancelled.json" '["error"]["code"]')" == "conflict" ]] \
  || { cat "$WORKDIR/bid-awcancelled.json"; fail "expected code=conflict"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from bids where id = '$gone_bid';")" == "Submitted" ]] \
  || fail "the refused award moved the bid"
ok "a job that has been cancelled can no longer be awarded, and the refused award leaves the offer where it was"

# ---------------------------------------------------------------------------------------
ticket "SHIP-93  the award closes every other live offer, and leaves the already-closed ones alone"

# # Two more providers, and every offer below is placed before the award for one reason
#
# **Once the job is Awarded it is offered to nobody**, so a provider asked to bid afterwards is
# refused by SHIP-81's eligibility filter with a 404 — a true answer to a different question that
# reads exactly like a broken fixture. SHIP-92's block met that once with one rival; a sweep needs a
# market, so the whole market is built first and awarded second.
#
# The accounts take 04162 and 04163, which is the 0416x block this file's header allocates to bidding.
status="$(post_json "verify-sweep-p2-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"sweep-second-$$@example.com\",\"phone\":\"04162$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/sweep-second.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/sweep-second.json"; fail "could not register the second sweep provider: $status"; }
sweep_second_id="$(json "$WORKDIR/sweep-second.json" '["id"]')"
sweep_second_token="$(mint_token "$sweep_second_id")"

status="$(post_json "verify-sweep-p3-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"sweep-third-$$@example.com\",\"phone\":\"04163$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/sweep-third.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/sweep-third.json"; fail "could not register the third sweep provider: $status"; }
sweep_third_id="$(json "$WORKDIR/sweep-third.json" '["id"]')"
sweep_third_token="$(mint_token "$sweep_third_id")"

"$PSQL" "$DATABASE_URL" -q -c \
  "update users set email_verified_at = now(), phone_verified_at = now()
     where id in ('$sweep_second_id', '$sweep_third_id');"
verify_provider "$sweep_second_id" "$sweep_third_id"

for pair in "$sweep_second_token:SWP102" "$sweep_third_token:SWP103"; do
  status="$(bid_post "${pair%%:*}" "verify-sweep-veh-${pair##*:}-$$" /v1/fleet/vehicles \
    "{\"registration\":\"${pair##*:}\",\"vehicle_type\":\"box_truck\",\"max_weight_kg\":1200,\"load_length_cm\":300,\"load_width_cm\":160,\"load_height_cm\":180}" \
    "vehicle-${pair##*:}")"
  [[ "$status" == "201" ]] || { cat "$WORKDIR/bid-vehicle-${pair##*:}.json"; fail "adding ${pair##*:} returned $status"; }

  status="$(curl -s -X PATCH -o "$WORKDIR/sweep-profile.json" -w '%{http_code}' \
    -H "$auth_header: Bearer ${pair%%:*}" -H "Idempotency-Key: verify-sweep-prof-${pair##*:}-$$" \
    -H 'Content-Type: application/json' -d '{"service_area":{"states":["VIC"]}}' \
    "http://localhost:$VERIFY_PORT/v1/fleet/profile")"
  [[ "$status" == "200" ]] || { cat "$WORKDIR/sweep-profile.json"; fail "declaring VIC returned $status"; }
done

sweep_job="$(new_award_job four)"

# A second job, with one live offer on it, standing beside the first for the whole of the award. The
# sweep's `WHERE job_id` is the one clause whose absence would be catastrophic *and* silent — every
# live offer in the marketplace closes on the first award of the day, no constraint is violated, and
# the customers whose jobs were emptied are not the ones who made the request.
bystander_job="$(new_award_job five)"
status="$(bid_post "$bid_provider_token" "verify-sweep-bystander-$$" "/v1/jobs/$bystander_job/bids" "$bid_body" swbystander)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-swbystander.json"; fail "the offer on the second job returned $status"; }
sweep_bystander_bid="$(json "$WORKDIR/bid-swbystander.json" '["id"]')"

# 1. The offer that wins.
status="$(bid_post "$bid_provider_token" "verify-sweep-winner-$$" "/v1/jobs/$sweep_job/bids" "$bid_body" swwinner)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-swwinner.json"; fail "the winning offer returned $status"; }
sweep_winner="$(json "$WORKDIR/bid-swwinner.json" '["id"]')"

# 2. A negotiation that loses, which is two rows and two different fates. The provider's first offer is
#    displaced by the customer's counter and is already terminal at Superseded; the counter is the live
#    head, it was made by the *customer*, and it is still a live offer on the job.
status="$(bid_post "$bid_rival_token" "verify-sweep-neg1-$$" "/v1/jobs/$sweep_job/bids" "$bid_body" swneg1)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-swneg1.json"; fail "the losing negotiation's first offer returned $status"; }
sweep_displaced="$(json "$WORKDIR/bid-swneg1.json" '["id"]')"

status="$(bid_post "$award_customer_token" "verify-sweep-neg2-$$" \
  "/v1/jobs/$sweep_job/bids/$sweep_displaced/counter" '{"amount_cents":41000}' swneg2)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-swneg2.json"; fail "the customer's counter returned $status"; }
sweep_counter="$(json "$WORKDIR/bid-swneg2.json" '["id"]')"

# 3. A plain competing offer.
status="$(bid_post "$sweep_second_token" "verify-sweep-live-$$" "/v1/jobs/$sweep_job/bids" "$bid_body" swlive)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-swlive.json"; fail "the competing offer returned $status"; }
sweep_live="$(json "$WORKDIR/bid-swlive.json" '["id"]')"

# 4. And one the provider had already taken back, which the award must not touch.
status="$(bid_post "$sweep_third_token" "verify-sweep-wd-$$" "/v1/jobs/$sweep_job/bids" "$bid_body" swwd)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-swwd.json"; fail "the offer to withdraw returned $status"; }
sweep_withdrawn="$(json "$WORKDIR/bid-swwd.json" '["id"]')"
status="$(bid_post "$sweep_third_token" "verify-sweep-wdo-$$" \
  "/v1/jobs/$sweep_job/bids/$sweep_withdrawn/withdraw" '{}' swwdo)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-swwdo.json"; fail "withdrawing returned $status"; }

before="$("$PSQL" "$DATABASE_URL" -tAc \
  "select string_agg(status, ',' order by status) from bids where job_id = '$sweep_job';")"
[[ "$before" == "Submitted,Submitted,Submitted,Superseded,Withdrawn" ]] \
  || fail "the market before the award reads '$before', want three live offers, one superseded and one withdrawn"
ok "five offers stand on one job — three live, one displaced by a counter, one withdrawn"

# The two timestamps that say whether a row was written at all. bids_set_updated_at moves on any
# write, so a sweep that rewrote a closed offer with the status it already had would still show here.
withdrawn_before="$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$sweep_withdrawn';")"
displaced_before="$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$sweep_displaced';")"

status="$(award "$award_customer_token" "verify-sweep-award-$$" "$sweep_job" "$sweep_winner" swaward)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-swaward.json"; fail "awarding returned $status, want 200"; }

# Every row of the job, by name, in one query. Read as a whole rather than one assertion per offer:
# what is being demonstrated is a *state of the job*, and five separate checks would pass individually
# on a sweep that closed four of the five.
after="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (select status from bids where id = '$sweep_winner')
       || ' ' || (select status from bids where id = '$sweep_counter')
       || ' ' || (select status from bids where id = '$sweep_live')
       || ' ' || (select status from bids where id = '$sweep_displaced')
       || ' ' || (select status from bids where id = '$sweep_withdrawn');")"
[[ "$after" == "Accepted Rejected Rejected Superseded Withdrawn" ]] \
  || fail "after the award the five offers read '$after', want 'Accepted Rejected Rejected Superseded Withdrawn'"
ok "the award accepts one offer and rejects every other live one — including the customer's own outstanding counter, which they decline by awarding elsewhere"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$sweep_withdrawn';")" == "$withdrawn_before" ]] \
  || fail "the sweep wrote to a withdrawn offer"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$sweep_displaced';")" == "$displaced_before" ]] \
  || fail "the sweep wrote to a superseded offer"
ok "and it does not touch an offer that had already ended — how an offer closed is the record, and no constraint would have refused overwriting it"

live_and_accepted="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) filter (where status = 'Submitted')::text || ' ' || count(*) filter (where status = 'Accepted')::text
     from bids where job_id = '$sweep_job';")"
[[ "$live_and_accepted" == "0 1" ]] \
  || fail "the awarded job reads '$live_and_accepted' (live, accepted), want '0 1'"
# Three since SHIP-90: the publication, the move to Negotiating the first of the five offers made,
# and the award. What the count is for is unchanged — a job at Awarded with a history that does not
# describe getting there is a status written round 000402's guard.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status || ' ' || (select count(*) from job_status_history where job_id = j.id)::text
     from jobs j where j.id = '$sweep_job';")" == "Awarded 3" ]] \
  || fail "the sweep did not happen inside the award's own transaction"
ok "nothing is left live on the awarded job, and the whole act is still one transition — Docs/02 §3's 'atomically', both halves"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from bids where id = '$sweep_bystander_bid';")" == "Submitted" ]] \
  || fail "the sweep closed an offer on another job entirely"
# Negotiating rather than Open since SHIP-90: the bystander has an offer of its own, which is what
# makes it a bystander worth having. The assertion is that this award reached neither its offer nor
# its status.
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$bystander_job';")" == "Negotiating" ]] \
  || fail "the award moved a job it was not made on"
ok "a live offer on another job is untouched — the sweep is scoped to the job that was awarded, and nothing would have said so if it were not"

# What the sweep buys the rest of the domain: the three verbs that gate on a live offer now all refuse.
# Before it, a provider could revise the price of an offer on a job somebody else had already won.
status="$(curl -s -X PATCH -o "$WORKDIR/bid-swrevise.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $sweep_second_token" -H "Idempotency-Key: verify-sweep-rev-$$" \
  -H 'Content-Type: application/json' -d '{"amount_cents":30000}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$sweep_job/bids/$sweep_live")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-swrevise.json"; fail "revising a closed offer returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-swrevise.json" '["error"]["code"]')" == "bidding_bid_closed" ]] \
  || { cat "$WORKDIR/bid-swrevise.json"; fail "expected code=bidding_bid_closed"; }

status="$(bid_post "$sweep_second_token" "verify-sweep-wdlost-$$" \
  "/v1/jobs/$sweep_job/bids/$sweep_live/withdraw" '{}' swwdlost)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-swwdlost.json"; fail "withdrawing a closed offer returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-swwdlost.json" '["error"]["code"]')" == "bidding_bid_closed" ]] \
  || { cat "$WORKDIR/bid-swwdlost.json"; fail "expected code=bidding_bid_closed"; }

status="$(bid_post "$award_customer_token" "verify-sweep-ctlost-$$" \
  "/v1/jobs/$sweep_job/bids/$sweep_live/counter" '{"amount_cents":30000}' swctlost)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-swctlost.json"; fail "countering a closed offer returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-swctlost.json" '["error"]["code"]')" == "bidding_bid_closed" ]] \
  || { cat "$WORKDIR/bid-swctlost.json"; fail "expected code=bidding_bid_closed"; }
ok "a losing offer can no longer be revised, withdrawn or countered — one code for all three, because the client does the same thing with all three"

# The record survives, which is the whole reason a rejection is a status rather than a delete. Both
# parties can still read the losing negotiation from end to end.
status="$(bid_get "$award_customer_token" "/v1/jobs/$sweep_job/bids/$sweep_counter/history" swhist)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-swhist.json"; fail "the losing negotiation's history returned $status"; }
[[ "$(python3 -c "import json,sys; d=json.load(open(sys.argv[1]))['data']; print(str(len(d)) + ' ' + d[0]['status'] + ' ' + d[1]['status'])" "$WORKDIR/bid-swhist.json")" == "2 superseded rejected" ]] \
  || { cat "$WORKDIR/bid-swhist.json"; fail "the losing negotiation is not readable end to end after the award"; }
ok "and the whole losing negotiation is still readable, each row saying how it ended — Docs/01 §4.3 records every offer, and a status is the record"

# ---------------------------------------------------------------------------------------
ticket "SHIP-94  a retried award returns the original outcome, through whichever mechanism is left"

# # There are two mechanisms and they answer different retries, so the checks tell them apart
#
# | Mechanism | What it guarantees | For how long |
# |---|---|---|
# | httpx.Idempotent (Redis) | the *response* is replayed and the handler never runs | while the entry lives — a TTL, an eviction, a failover |
# | the record itself | the *act* cannot happen twice | permanently |
#
# SHIP-111 made the same split and named the honest version of it: **Redis makes the retry cheap, the
# database makes it correct.** The difference here is that the award needs no stored key to be correct,
# because it is an `UPDATE` of a row that already exists — there is no second row a retry could create,
# so the accepted status *is* the record of the request. That is SHIP-86's idempotency by state, and it
# is what a phone that reconnected, restarted and generated a **fresh** key falls back on.
#
# `Idempotency-Replayed` on the wire is what makes the claim checkable rather than asserted: it is
# present when Redis answered and absent when the handler did.
idem_job="$(new_award_job six)"
idem_key="verify-award-idem-$$"

status="$(bid_post "$bid_provider_token" "verify-idem-winner-$$" "/v1/jobs/$idem_job/bids" "$bid_body" idwinner)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-idwinner.json"; fail "the offer to award returned $status"; }
idem_winner="$(json "$WORKDIR/bid-idwinner.json" '["id"]')"

# Placed before the award, for the reason the sweep's fixture is: an Awarded job is offered to nobody.
status="$(bid_post "$bid_rival_token" "verify-idem-loser-$$" "/v1/jobs/$idem_job/bids" "$bid_body" idloser)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-idloser.json"; fail "the competing offer returned $status"; }
idem_loser="$(json "$WORKDIR/bid-idloser.json" '["id"]')"

status="$(award "$award_customer_token" "$idem_key" "$idem_job" "$idem_winner" idfirst)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-idfirst.json"; fail "the award returned $status, want 200"; }
replayed_from_redis idfirst && fail "the first award was answered from the cache"
idem_winner_written="$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$idem_winner';")"
idem_loser_written="$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$idem_loser';")"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from bids where id = '$idem_loser';")" == "Rejected" ]] \
  || fail "the award did not close the competing offer"
ok "the award runs once, accepting one offer and closing the other"

# 1. The same key while Redis still holds it. The handler is never reached, so this proves nothing
#    about the domain — which is exactly why the next check deletes the entry.
status="$(award "$award_customer_token" "$idem_key" "$idem_job" "$idem_winner" idreplay)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-idreplay.json"; fail "the cached retry returned $status"; }
replayed_from_redis idreplay || fail "the immediate retry was not replayed by the middleware"
diff -q "$WORKDIR/bid-idfirst.json" "$WORKDIR/bid-idreplay.json" >/dev/null \
  || fail "the replayed response is not byte-identical to the original"
ok "the same key immediately after replays the stored response from Redis, byte for byte — the handler is never reached"

# 2. The entry deleted, which is what a TTL expiry, an eviction or a failover looks like from the
#    handler's side. **This is the check the ticket turns on**: the request now runs a second time, all
#    the way into the transaction, and has to answer the same thing having written nothing.
forget_the_cached_response "$award_customer_id" "$idem_key"
status="$(award "$award_customer_token" "$idem_key" "$idem_job" "$idem_winner" idforgotten)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-idforgotten.json"; fail "the retry after the cached response expired returned $status, want 200"; }
replayed_from_redis idforgotten && fail "the entry was deleted and the middleware still replayed; this check is proving nothing"
[[ "$(json "$WORKDIR/bid-idforgotten.json" '["id"]')" == "$idem_winner" ]] \
  || fail "the retry answered with a different offer"
[[ "$(json "$WORKDIR/bid-idforgotten.json" '["status"]')" == "accepted" ]] \
  || fail "the retry did not answer with the accepted offer"
ok "with the cached response gone the request runs again, reaches the transaction, and is answered from the row it already wrote"

# 3. And it wrote nothing — including the sweep, which is the write no caller ever names. Both
#    timestamps, because bids_set_updated_at moves on any write to a row even when the status it sets
#    is the status already there.
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$idem_winner';")" == "$idem_winner_written" ]] \
  || fail "the retry rewrote the accepted offer"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select updated_at from bids where id = '$idem_loser';")" == "$idem_loser_written" ]] \
  || fail "the retry ran the rejection sweep a second time"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status || ' ' || (select count(*) from job_status_history where job_id = j.id)::text
     from jobs j where j.id = '$idem_job';")" == "Awarded 3" ]] \
  || fail "the retries left more than one Awarded transition on the job — three is its publication, the move to Negotiating an offer made (SHIP-90), and the award"
ok "and it recorded nothing further — not the accept, not the sweep, not a second transition"

# 4. The two mechanisms disagreeing, which is the case worth naming. The **same key** carrying a
#    **different offer** is not a retry at all: the middleware fingerprints method, path and body, so
#    it refuses rather than replaying — answering with the first request's response would tell a client
#    that something it never sent had succeeded. The record would have said "that job is already
#    awarded", which is also true; the key wins because it is in front, and because "you have reused a
#    key" is the more actionable of the two.
status="$(award "$award_customer_token" "$idem_key" "$idem_job" "$idem_loser" idreused)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-idreused.json"; fail "one key carrying two different awards returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-idreused.json" '["error"]["code"]')" == "idempotency_key_reused" ]] \
  || { cat "$WORKDIR/bid-idreused.json"; fail "expected code=idempotency_key_reused"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids where job_id = '$idem_job' and status = 'Accepted';")" == "1" ]] \
  || fail "the reused key awarded a second offer"
ok "the same key naming a different offer is refused rather than replayed — a key identifies a request, not an intention"

# And the answer the record would have given, under a key of its own, so both halves are on the record.
status="$(award "$award_customer_token" "verify-award-idem-other-$$" "$idem_job" "$idem_loser" idother)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-idother.json"; fail "awarding a second offer returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-idother.json" '["error"]["code"]')" == "conflict" ]] \
  || { cat "$WORKDIR/bid-idother.json"; fail "expected code=conflict"; }
ok "under a key of its own that same request is a conflict — the two mechanisms refuse it for different reasons and the middleware's is the one in front"

# 5. The key is namespaced by the authenticated subject (SHIP-44), which is the gate CLAUDE.md holds
#    every authenticated state-changing endpoint behind. A stranger sending this customer's key gets
#    their own refusal rather than this customer's stored 200 — the cross-tenant read that
#    idem:v1:anonymous:<key> would have made possible.
status="$(award "$award_stranger_token" "$idem_key" "$idem_job" "$idem_winner" idstranger)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-idstranger.json"; fail "a stranger reusing the key returned $status, want 404"; }
replayed_from_redis idstranger && fail "a stranger was handed this customer's stored response"
ok "and another caller sending the same key is answered in their own scope, never from this customer's stored response"

# ---------------------------------------------------------------------------------------
ticket "SHIP-95  two awards arriving together leave exactly one accepted offer"

# SHIP-95's races are pinned in Go, where a test can hold one transaction open and ask PostgreSQL
# whether the other is waiting on it — see internal/bidding/race_test.go. This is the one assertion
# that cannot be made there: the same race through the **served binary**, against cmd/api's own
# `LockForAward` rather than the copy the package's fixtures carry.
#
# **What is asserted is the outcome invariant, not the interleaving.** Two curls started together may
# or may not overlap on any given run, and a shell check that depended on them overlapping would be
# flaky rather than strict. Either way exactly one award may succeed — Docs/01 §6, "a job can have one
# accepted bid only" — and the loser is `conflict` rather than `bidding_bid_closed`, because the sweep
# has made "that offer is closed" true of every offer on the job and only "you have already awarded
# this job" tells the client where to look.
#
# The fixture is a job of its own. The sections above have already awarded theirs, and a race needs a
# job with two live offers on it.

"$PSQL" "$DATABASE_URL" -q -c \
  "update users set email_verified_at = now(), phone_verified_at = now()
     where id in ('$bid_provider_id', '$bid_rival_id');"

status="$(bid_post "$bid_customer_token" "verify-race-job-$$" /v1/jobs \
  '{"pickup":{"line":"5 Church Street","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"1 Bourke Street","suburb":"Melbourne","state":"VIC","postcode":"3000"},
    "goods_description":"Two-seater sofa","weight_kg":80,"length_cm":190,"width_cm":90,"height_cm":80,
    "budget_cents":432199}' race-job)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-race-job.json"; fail "creating the race job returned $status"; }
race_job_id="$(json "$WORKDIR/bid-race-job.json" '["id"]')"
move_job "$race_job_id" Draft Open

status="$(bid_post "$bid_provider_token" "verify-race-bid-a-$$" "/v1/jobs/$race_job_id/bids" "$bid_body" race-bid-a)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-race-bid-a.json"; fail "the first racer's offer returned $status"; }
race_bid_a="$(json "$WORKDIR/bid-race-bid-a.json" '["id"]')"

status="$(bid_post "$bid_rival_token" "verify-race-bid-b-$$" "/v1/jobs/$race_job_id/bids" "$bid_body" race-bid-b)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-race-bid-b.json"; fail "the second racer's offer returned $status"; }
race_bid_b="$(json "$WORKDIR/bid-race-bid-b.json" '["id"]')"

# race_award <key> <bid> <name> — one award, writing its status to a file so a background job can
# report it. Distinct keys deliberately: under one key the middleware refuses the second request with
# `idempotency_request_in_progress` and the platform below it never sees two.
race_award() {
  curl -s -X POST -o "$WORKDIR/bid-$3.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $bid_customer_token" -H "Idempotency-Key: $1" \
    -H 'Content-Type: application/json' -d "{\"bid_id\":\"$2\"}" \
    "http://localhost:$VERIFY_PORT/v1/jobs/$race_job_id/award" > "$WORKDIR/bid-$3.status"
}

race_award "verify-race-a-$$" "$race_bid_a" race-a &
race_pid_a=$!
race_award "verify-race-b-$$" "$race_bid_b" race-b &
race_pid_b=$!
wait "$race_pid_a" || fail "the first concurrent award never answered"
wait "$race_pid_b" || fail "the second concurrent award never answered"

race_won=0
race_refused=0
for race_name in race-a race-b; do
  race_status="$(< "$WORKDIR/bid-$race_name.status")"
  case "$race_status" in
    200) race_won=$((race_won + 1)) ;;
    409)
      race_refused=$((race_refused + 1))
      race_code="$(json "$WORKDIR/bid-$race_name.json" '["error"]["code"]')"
      [[ "$race_code" == "conflict" ]] \
        || fail "the losing award answered $race_code, want conflict — a second award is a job that has moved on, not an offer that closed"
      ;;
    *) cat "$WORKDIR/bid-$race_name.json"; fail "a concurrent award returned $race_status, want 200 or 409" ;;
  esac
done
[[ "$race_won" == 1 ]] || fail "$race_won of 2 concurrent awards succeeded, want exactly 1"
[[ "$race_refused" == 1 ]] || fail "$race_refused of 2 concurrent awards were refused, want exactly 1"
ok "two awards on one job answer one 200 and one 409 conflict, whichever arrives first"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids where job_id = '$race_job_id' and status = 'Accepted';")" == "1" ]] \
  || fail "the race left more or fewer than one accepted offer — two providers each believing they have the job is a marketplace-credibility failure"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids where job_id = '$race_job_id' and status = 'Submitted';")" == "0" ]] \
  || fail "an offer is still live on an awarded job"
ok "the database holds exactly one accepted offer and nothing still live, which is what uq_bids_one_accepted_per_job is for"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$race_job_id';")" == "Awarded" ]] \
  || fail "the job did not reach Awarded"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$race_job_id' and to_status = 'Awarded';")" == "1" ]] \
  || fail "the job was moved to Awarded more than once — the losing award ran the transition as well"
ok "and the job was moved to Awarded exactly once, through the guarded transition"

# ---------------------------------------------------------------------------------------
ticket "SHIP-101a  GET /v1/fleet/bids — a provider reads their own bids, and nobody else's"

# SHIP-101a's *Done when*: "a provider lists every bid they have placed, grouped by status,
# paginated, and sees no other provider's; the response carries no customer budget in any form."
#
# The read SHIP-101's screen had nothing to work from without. Docs/11 §6 struck that ticket for two
# waves as "every dependency met and unbuildable in fact", because the only bidding read on the
# served surface needed a job identifier and a bid identifier the provider would have to hold
# already.
#
# # What only this can show
#
# internal/bidding/read_test.go holds the scope, the status filter, the keyset and the closed key
# set. **What only this can show is the route being served at all** — under `/v1/fleet` rather than
# under a job, past the real auth class, against rows every earlier section in this file placed
# through real endpoints.
#
# Both providers registered at the top have bid on several jobs by now, which is what makes the
# "sees no other provider's" check a real one rather than a comparison of two empty lists.

bid_list() {
  curl -s -o "$WORKDIR/bid-list-$3.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" "http://localhost:$VERIFY_PORT/v1/fleet/bids$2"
}

status="$(curl -s -o "$WORKDIR/bid-list-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/fleet/bids")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/bid-list-anon.json"; fail "an unauthenticated bid list returned $status, want 401"; }
ok "it cannot be reached without a credential — the provider is the token's, not a parameter's"

status="$(bid_list "$bid_provider_token" "" mine)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-list-mine.json"; fail "listing a provider's own bids returned $status"; }

python3 - "$WORKDIR/bid-list-mine.json" <<'PY' || fail "the bid list is not the collection envelope"
import json, sys

page = json.load(open(sys.argv[1]))
if sorted(page) not in (["data", "has_more"], ["data", "has_more", "next_cursor"]):
    sys.exit("the envelope carries %s" % sorted(page))
if not isinstance(page["data"], list) or not page["data"]:
    sys.exit("the list is empty, and this provider has bid several times through this file")
PY
ok "it answers the collection envelope of Docs/10 §4.5, with the offers this file placed in it"

# **Every row belongs to the calling provider.** This is the one endpoint in the domain that answers
# with a *set* rather than a row somebody named, so the scope is the query rather than a refusal —
# delete `provider_id` from its WHERE clause and nothing else in this file notices.
mine_ids="$(python3 -c '
import json, sys
print(" ".join(row["id"] for row in json.load(open(sys.argv[1]))["data"]))
' "$WORKDIR/bid-list-mine.json")"
for listed in $mine_ids; do
  [[ "$("$PSQL" "$DATABASE_URL" -tAc "select provider_id from bids where id = '$listed';")" == "$bid_provider_id" ]] \
    || fail "the list carries $listed, which belongs to another provider — Docs/01 §4.3 keeps a competitor's price private"
done

status="$(bid_list "$bid_rival_token" "" rival)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-list-rival.json"; fail "the rival's list returned $status"; }
rival_ids="$(python3 -c '
import json, sys
print(" ".join(row["id"] for row in json.load(open(sys.argv[1]))["data"]))
' "$WORKDIR/bid-list-rival.json")"
[[ -n "$rival_ids" ]] || fail "the rival's list is empty, so the comparison below proves nothing"
for listed in $rival_ids; do
  case " $mine_ids " in
    *" $listed "*) fail "$listed is in both providers' lists" ;;
  esac
done
ok "each provider's list holds only their own negotiations, and the two do not overlap"

# The customer's counter-offers are in the provider's list and are told apart by `offered_by`, which
# is the reading read.go argues: a counter carries the provider's id and is the row waiting for an
# answer from them.
python3 - "$WORKDIR/bid-list-mine.json" <<'PY' || fail "the list does not distinguish who made each offer"
import json, sys

rows = json.load(open(sys.argv[1]))["data"]
parties = {row["offered_by"] for row in rows}
if not parties <= {"provider", "customer"}:
    sys.exit("offered_by holds %s" % sorted(parties))
if not all("status" in row for row in rows):
    sys.exit("a row carries no status, so a client cannot group by one")
PY
ok "every row says which party offered it and what status it is in — enough to group by status client-side"

# Grouping, the other way round: ask the platform for one group.
status="$(bid_list "$bid_provider_token" "?status=withdrawn" withdrawn)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-list-withdrawn.json"; fail "?status=withdrawn returned $status"; }
python3 - "$WORKDIR/bid-list-withdrawn.json" <<'PY' || fail "?status= did not narrow the list"
import json, sys

rows = json.load(open(sys.argv[1]))["data"]
if not rows:
    sys.exit("the withdrawn group is empty, and this file withdrew an offer through the endpoint")
wrong = {row["status"] for row in rows} - {"withdrawn"}
if wrong:
    sys.exit("the withdrawn group also holds %s" % sorted(wrong))
PY
ok "?status= narrows to one group, and the filter runs in the query rather than over the page"

status="$(bid_list "$bid_provider_token" "?status=haggling" badstatus)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-list-badstatus.json"; fail "an unknown status returned $status, want 400"; }
[[ "$(json "$WORKDIR/bid-list-badstatus.json" '["error"]["code"]')" == "bad_request" ]] \
  || fail "an unknown status answered $(json "$WORKDIR/bid-list-badstatus.json" '["error"]["code"]')"
status="$(bid_list "$bid_provider_token" "?cursor=not-a-cursor" badcursor)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-list-badcursor.json"; fail "a mangled cursor returned $status, want 400"; }
ok "an unknown status and a cursor this endpoint never issued are both refused rather than ignored"

# Paging, over rows this file created: one at a time, following the cursor, with nothing repeated
# and nothing dropped. The tie-break is the half worth having — several of these offers were written
# in the same second.
status="$(bid_list "$bid_provider_token" "?limit=1" page1)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-list-page1.json"; fail "the first page returned $status"; }
[[ "$(json "$WORKDIR/bid-list-page1.json" '["has_more"]')" == "True" ]] \
  || fail "a one-row page over several offers reports no further pages"
page_cursor="$(json "$WORKDIR/bid-list-page1.json" '["next_cursor"]')"
[[ -n "$page_cursor" ]] || fail "the first page carries no cursor"

status="$(bid_list "$bid_provider_token" "?limit=1&cursor=$page_cursor" page2)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-list-page2.json"; fail "the second page returned $status"; }
[[ "$(json "$WORKDIR/bid-list-page2.json" '["data"][0]["id"]')" != "$(json "$WORKDIR/bid-list-page1.json" '["data"][0]["id"]')" ]] \
  || fail "the second page repeated the first page's row"
ok "it pages by cursor, and the second page starts after the first rather than repeating it"

# CLAUDE.md's budget invariant, on the newest provider-facing response. The fixture jobs all carry
# one, which is what makes this a real assertion rather than a search over rows with nothing to leak.
python3 - "$WORKDIR/bid-list-mine.json" <<'PY' || fail "a listed bid carries something of the customer's"
import json, sys

# The same closed key set the single-bid checks above use. A field named `max_price` is a budget and
# does not contain the word, which is the axis a search for "budget" cannot have.
permitted = {
    "id", "job_id", "status", "offered_by", "amount_cents",
    "pickup_at", "deliver_by", "message", "superseded_by", "created_at", "updated_at",
}
for row in json.load(open(sys.argv[1]))["data"]:
    extra = set(row) - permitted
    if extra:
        sys.exit("a listed bid carries %s" % sorted(extra))
PY
case "$(tr 'A-Z' 'a-z' <"$WORKDIR/bid-list-mine.json")" in
  *budget*|*432199*|*4321.99*) fail "the bid list body mentions the customer's budget" ;;
esac
ok "and no row carries the customer's budget in any form, on jobs that all have one"

unset mine_ids rival_ids page_cursor listed
unset -f bid_list

# ---------------------------------------------------------------------------------------
ticket "SHIP-89  a bid expires on its own terms, and the sweep is the real worker binary"

# Docs/01 §4.2 bounds a provider's three verbs "until it is accepted or expires", and SHIP-89 is
# what makes the second half true. **An offer's own terms are the collection time it committed
# to**: once `pickup_at` has passed, awarding the offer would commit a provider to collecting in
# the past, which is what the placement validator already refuses. Migration 000503 argues down the
# alternative — a `bids.expires_at` with a fixed lifetime — and internal/bidding/expiry.go carries
# the reasoning.
#
# # What is demonstrated here and what is demonstrated by tests
#
# internal/bidding/expiry_test.go holds which offers are due, the event, and every closed status
# the sweep must leave alone. cmd/worker/tasks_bidding_test.go holds the pass: the registration,
# the claim, two workers dividing the work, and a failed pass releasing everything.
#
# **What only this can show is the real binary sweeping rows the served API wrote**, and the
# consequence a client meets afterwards — an expired offer that can be neither revised nor awarded,
# through the same endpoints that would have taken it an hour earlier.
#
# # Two rules from the harness header apply, and the second one is where this section had to think
#
# **Fence what you assert on.** Every query below names a bid or a job this section created. This
# database persists between runs, so a count over `bids WHERE status = 'Expired'` would include
# every earlier run's sweep.
#
# **Own what you assert about.** cmd/worker is one binary and a start runs *every* registered task,
# so this start also runs `job-expiry`, `job-expiry-warning` and `outbox-publisher`. The two job
# sweeps are safe by construction: both jobs below are published with no pickup window, so 000406
# gives them the fourteen-day backstop and neither is due nor inside the warning window.
#
# **`outbox-publisher` is not**, and that is worth stating rather than working around silently.
# 80-notifications.sh reads the bid and delivery rows this file and 70-delivery.sh deliberately
# leave unpublished, captured by id *before* its own worker start. A drain here would publish them
# early and that section's `shipper.bid` assertion would quietly stop covering anything — the same
# shape as SHIP-135's section deleting a topic another section reads, which the harness resolves by
# ordering. Ordering cannot help here: this is section 61 and that is section 80.
#
# So the worker below is pointed at a broker that is not there. Every outbox pass then fails
# legibly and leaves each row claimable, which is `outboxDrain`'s documented behaviour under an
# unreachable broker rather than a trick — SHIP-134's own test asserts it. The three tasks that
# matter here are claims against PostgreSQL and need no broker at all.
#
# **The finding behind that paragraph belongs in Docs/11 §9 rather than only here.** SHIP-15r's
# convention says a section leaves nothing *due*; `outbox-publisher` has no "not due" state,
# because every unpublished row is due the moment it is written. It is already the case §9 named as
# the reopening trigger, and it has been since wave 4.

bid_expiry_pickup="$(date -u -v+2d '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d '+2 days' '+%Y-%m-%dT%H:%M:%SZ')"
bid_expiry_deliver="$(date -u -v+3d '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d '+3 days' '+%Y-%m-%dT%H:%M:%SZ')"
bid_expiry_body="{\"amount_cents\":51000,\"pickup_at\":\"$bid_expiry_pickup\",\"deliver_by\":\"$bid_expiry_deliver\"}"

# new_bid_job <name> — one Open job of this section's own, with no pickup window.
#
# No window on purpose: 000406 then gives the job the fourteen-day backstop rather than a deadline
# two days out, which is what keeps the two job sweeps in the same binary away from it.
new_bid_job() {
  local status
  status="$(bid_post "$bid_customer_token" "verify-bid-expiry-job-$1-$$" /v1/jobs \
    '{"pickup":{"line":"5 Church Street","suburb":"Richmond","state":"VIC","postcode":"3121"},
      "dropoff":{"line":"1 Bourke Street","suburb":"Melbourne","state":"VIC","postcode":"3000"},
      "goods_description":"Two-seater sofa","weight_kg":80,"length_cm":190,"width_cm":90,"height_cm":80,
      "budget_cents":432199}' "expiry-job-$1")"
  [[ "$status" == "201" ]] || { cat "$WORKDIR/bid-expiry-job-$1.json"; fail "creating the $1 job returned $status"; }
  local id
  id="$(json "$WORKDIR/bid-expiry-job-$1.json" '["id"]')"
  move_job "$id" Draft Open
  printf '%s' "$id"
}

# age_offer <bid> — move an offer's collection time an hour into the past.
#
# Written with SQL because **no endpoint can produce a due offer**: the validator refuses a
# `pickup_at` that is not in the future, on a placement and on a revision alike. That refusal is
# the reason this sweep is safe to register at all, and it is also why a section demonstrating the
# sweep has to age a row rather than write one. `deliver_by` moves with it, because
# ck_bids_timing_is_ordered wants delivery after collection.
age_offer() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
    "update bids set pickup_at = now() - interval '1 hour', deliver_by = now() + interval '7 hours'
       where id = '$1';"
}

bid_expiry_job="$(new_bid_job due)"
bid_untouched_job="$(new_bid_job live)"

status="$(bid_post "$bid_provider_token" "verify-bid-expiry-due-$$" \
  "/v1/jobs/$bid_expiry_job/bids" "$bid_expiry_body" expiry-due)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-expiry-due.json"; fail "placing the offer to expire returned $status"; }
bid_expiry_due="$(json "$WORKDIR/bid-expiry-due.json" '["id"]')"

status="$(bid_post "$bid_rival_token" "verify-bid-expiry-gone-$$" \
  "/v1/jobs/$bid_expiry_job/bids" "$bid_expiry_body" expiry-gone)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-expiry-gone.json"; fail "placing the offer to withdraw returned $status"; }
bid_expiry_gone="$(json "$WORKDIR/bid-expiry-gone.json" '["id"]')"

status="$(bid_post "$bid_rival_token" "verify-bid-expiry-live-$$" \
  "/v1/jobs/$bid_untouched_job/bids" "$bid_expiry_body" expiry-live)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-expiry-live.json"; fail "placing the live offer returned $status"; }
bid_expiry_live="$(json "$WORKDIR/bid-expiry-live.json" '["id"]')"

# Withdrawn *before* it is aged, so the sweep meets a closed offer whose collection time has also
# passed. That is the case with no constraint behind it: nothing in the schema refuses `Expired`
# over `Withdrawn`, and overwriting it would replace the record of *how* the offer ended with the
# record of when.
status="$(bid_post "$bid_rival_token" "verify-bid-expiry-wdraw-$$" \
  "/v1/jobs/$bid_expiry_job/bids/$bid_expiry_gone/withdraw" '{}' expiry-withdraw)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-expiry-withdraw.json"; fail "withdrawing returned $status"; }

age_offer "$bid_expiry_due"
age_offer "$bid_expiry_gone"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids where id in ('$bid_expiry_due', '$bid_expiry_gone', '$bid_expiry_live')
     and status in ('Submitted', 'Withdrawn');")" == "3" ]] \
  || fail "the expiry fixture is not in the state the sweep is about to be judged on"

pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-worker-bids" ./cmd/worker
popd >/dev/null
ok "the worker builds with the bidding domain's task registered"

# KAFKA_BROKERS names a port nothing listens on, deliberately — see the note above. The outbox pass
# fails and leaves every row claimable for 80-notifications.sh; the three claim-based tasks run
# normally.
SHIPPER_ENV=development \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
KAFKA_BROKERS=localhost:1 \
  "$WORKDIR/shipper-worker-bids" >"$WORKDIR/worker-bids.log" 2>&1 &
bid_worker_pid=$!

for _ in $(seq 1 100); do
  bid_expiry_status="$("$PSQL" "$DATABASE_URL" -tAc "select status from bids where id = '$bid_expiry_due';")"
  [[ "$bid_expiry_status" == "Expired" ]] && break
  sleep 0.2
done

kill -TERM "$bid_worker_pid" 2>/dev/null || true
for _ in $(seq 1 50); do
  kill -0 "$bid_worker_pid" 2>/dev/null || break
  sleep 0.2
done
wait "$bid_worker_pid" 2>/dev/null || true

[[ "$bid_expiry_status" == "Expired" ]] \
  || { cat "$WORKDIR/worker-bids.log"; fail "the offer past its collection time is $bid_expiry_status after a pass, want Expired"; }
ok "one pass of the real worker expires the offer whose collection time has passed"

grep -q '"task":"bid-expiry"' "$WORKDIR/worker-bids.log" \
  || { cat "$WORKDIR/worker-bids.log"; fail "the bidding domain's task did not register in the manifest"; }
ok "and it is registered as bid-expiry, the fourth task in a binary that runs all of them"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from bids where id = '$bid_expiry_live';")" == "Submitted" ]] \
  || fail "the sweep took an offer collecting in two days"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from bids where id = '$bid_expiry_gone';")" == "Withdrawn" ]] \
  || fail "the sweep wrote Expired over a withdrawn offer, replacing the record of how it ended"
ok "it leaves alone an offer that is not yet due, and one that has already closed some other way"

# The event, fenced on the one bid rather than counted over the topic or the table.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox
    where aggregate_type = 'bid' and aggregate_id = '$bid_expiry_due' and event_type = 'bid.expired';")" == "1" ]] \
  || fail "the expiry emitted no bid.expired event, which is the half of SHIP-89's Done when nothing else here would catch"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select payload->>'status' from outbox
    where aggregate_type = 'bid' and aggregate_id = '$bid_expiry_due' and event_type = 'bid.expired';")" == "Expired" ]] \
  || fail "the bid.expired payload does not carry the status the row now has"
ok "and it emits bid.expired from the domain, in the transaction that wrote the status"

# What a client meets afterwards. Both refusals go through [Status.live], so neither names expiry —
# which is the point: an expired offer is over in exactly the way a withdrawn or superseded one is.
status="$(curl -s -X PATCH -o "$WORKDIR/bid-expiry-revise.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $bid_provider_token" -H "Idempotency-Key: verify-bid-expiry-revise-$$" \
  -H 'Content-Type: application/json' -d '{"amount_cents":40000}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$bid_expiry_job/bids/$bid_expiry_due")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-expiry-revise.json"; fail "revising an expired offer returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-expiry-revise.json" '["error"]["code"]')" == "bidding_bid_closed" ]] \
  || fail "revising an expired offer answered $(json "$WORKDIR/bid-expiry-revise.json" '["error"]["code"]')"

status="$(bid_post "$bid_customer_token" "verify-bid-expiry-award-$$" \
  "/v1/jobs/$bid_expiry_job/award" "{\"bid_id\":\"$bid_expiry_due\"}" expiry-award)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/bid-expiry-award.json"; fail "awarding an expired offer returned $status, want 409"; }
[[ "$(json "$WORKDIR/bid-expiry-award.json" '["error"]["code"]')" == "bidding_bid_closed" ]] \
  || fail "awarding an expired offer answered $(json "$WORKDIR/bid-expiry-award.json" '["error"]["code"]')"
ok "an expired offer can be neither revised nor awarded, and both refusals are the closed-offer code"

# The wire form, read through the endpoint a provider actually has for it.
status="$(curl -s -o "$WORKDIR/bid-expiry-history.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $bid_provider_token" \
  "http://localhost:$VERIFY_PORT/v1/jobs/$bid_expiry_job/bids/$bid_expiry_due/history")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-expiry-history.json"; fail "reading the history returned $status"; }
[[ "$(json "$WORKDIR/bid-expiry-history.json" '["data"][0]["status"]')" == "expired" ]] \
  || fail "the expired offer reads as $(json "$WORKDIR/bid-expiry-history.json" '["data"][0]["status"]') on the wire"
ok "and it reads as \"expired\" in the lower snake case Docs/10 §4.7 requires"

# uq_bids_one_submitted_per_provider_per_job is partial on Submitted, so an expired offer leaves the
# predicate — which is what 000502 predicted this ticket would need and why it needed no change to
# the constraint. The same property a withdrawal has had since SHIP-86.
status="$(bid_post "$bid_provider_token" "verify-bid-expiry-again-$$" \
  "/v1/jobs/$bid_expiry_job/bids" "$bid_expiry_body" expiry-again)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-expiry-again.json"; fail "bidding again after an expiry returned $status, want 201"; }
ok "and the provider whose offer expired may place another, exactly as a withdrawal frees them to"

unset bid_expiry_pickup bid_expiry_deliver bid_expiry_body bid_expiry_status bid_worker_pid
unset -f new_bid_job age_offer

# ---------------------------------------------------------------------------------------
ticket "SHIP-136  every bid state change emits its event from the domain, into the outbox"

# Docs/01 §4.5: "new bid, counter-offer, withdrawal, or bid expiry" and "bid accepted".
#
# # What is demonstrated here and what is demonstrated by tests
#
# internal/bidding/events_test.go holds the payloads, the retry paths that must emit *once*, and the
# rolled-back transaction that must emit nothing. cmd/api/events_domain_test.go holds the half of the
# *Done when* that is about where the emit lives. None of that needs a running service.
#
# **What only this can show is that every offer, counter, withdrawal and award made through the
# served binary above left its event behind.** Every row asserted below was written by a real
# endpoint in an earlier section of this file, through cmd/api's own wiring rather than through a
# fixture's — which is the one thing a Go test in the package cannot reach. 80-notifications.sh takes
# them the rest of the way onto `shipper.bid`.
#
# # The fence
#
# `bids.provider_id` is the provider a negotiation is *with*, so every row this file wrote — including
# the customer's counter-offers, which carry the same provider id (000502) — belongs to one of the two
# providers registered at the top. Both are registered fresh with `$$` in the address, so the fence
# names this run and nothing else. **A count over `aggregate_type = 'bid'` would include every
# previous `make verify` on this database**, which persists between runs.
#
# Nothing here reads Kafka, and nothing here starts the worker: these rows are deliberately left
# unpublished for 80-notifications.sh to drain.

bid_event_fence="aggregate_type = 'bid' AND aggregate_id IN (
  SELECT id FROM bids WHERE provider_id IN ('$bid_provider_id', '$bid_rival_id'))"

# bid_events_of <event_type> — how many of one event type this run's offers produced.
bid_events_of() {
  "$PSQL" "$DATABASE_URL" -tAc \
    "select count(*) from outbox where $bid_event_fence and event_type = '$1';"
}

for bid_event in bid.placed bid.revised bid.withdrawn bid.countered bid.accepted bid.rejected; do
  [[ "$(bid_events_of "$bid_event")" -ge 1 ]] \
    || fail "no $bid_event reached the outbox, and this file made one through the served endpoint"
done
ok "a placement, a revision, a withdrawal, a counter, an award and its rejection sweep each emitted their event"

# **The sixth line of Docs/01 §4.5's bid list, and the handoff SHIP-136 left working.** This used to
# be the opposite assertion — that no bid is 'Expired' — placed here so that the day something wrote
# the first one it would fail and name SHIP-89. That is exactly what happened, and the check is now
# the positive form: the fourth event type reaches the outbox from a sweep the section above ran
# through the real worker binary.
#
# Fenced by the same two providers as the loop above rather than counted over `bids`, because this
# database persists between runs and every earlier run's sweep is still in the table.
[[ "$(bid_events_of bid.expired)" -ge 1 ]] \
  || fail "no bid.expired reached the outbox, and the SHIP-89 section above expired an offer through the worker"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from bids
     where provider_id in ('$bid_provider_id', '$bid_rival_id') and status = 'Expired';")" -ge 1 ]] \
  || fail "no offer of this run's is Expired, so the check above is asserting against an older run"
ok "and bid.expired is among them, from the scheduled sweep rather than from an endpoint"

# The aggregate is the bid, which is what makes one offer's events ordered against each other on the
# topic. An event whose payload named a different row from its aggregate id would key onto the wrong
# partition and be read by nobody looking for it.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox where $bid_event_fence and payload->>'bid_id' <> aggregate_id::text;")" == "0" ]] \
  || fail "a bid event is keyed on an aggregate its payload does not name"
ok "each event is keyed on the bid it is about, so one offer's events share a partition and keep their order"

# The award, specifically: the accepted offer and every offer it closed, from the one transaction —
# and the job's own transition beside them, emitted by `jobs` from inside the same transaction
# through a port, with neither domain importing the other.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox where aggregate_id = '$race_bid_a' and event_type in ('bid.accepted','bid.rejected');")" == "1" ]] \
  || fail "the first racer's offer has no accepted or rejected event"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox where aggregate_id = '$race_bid_b' and event_type in ('bid.accepted','bid.rejected');")" == "1" ]] \
  || fail "the second racer's offer has no accepted or rejected event"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox
     where aggregate_type = 'job' and aggregate_id = '$race_job_id' and event_type = 'job.status_changed';")" -ge 1 ]] \
  || fail "the award moved the job and `jobs` emitted nothing about it"
ok "an award emits the acceptance, one rejection per offer it closed, and the job's own transition — one transaction, two domains"

# CLAUDE.md's budget invariant, applied to the copy of a job that travels furthest. The fixture jobs
# above all carry a budget, so this is a real assertion rather than one over rows that have none.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox where $bid_event_fence and payload::text ilike '%budget%';")" == "0" ]] \
  || fail "a bid event payload mentions a budget — an event travels past every point a response body could have redacted it"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from jobs where id = '$race_job_id' and budget is not null;")" == "1" ]] \
  || fail "the fixture job carries no budget, so the check above proves nothing"
ok "and no bid event carries the customer's budget, on jobs that have one"

unset bid_event bid_event_fence
unset -f bid_events_of

# ---------------------------------------------------------------------------------------
ticket "SHIP-102a  GET /v1/jobs/{id}/bids/received — a customer reads the offers on their own job"

# SHIP-102a's *Done when*: "the owning customer lists every live offer on one of their jobs in the
# Docs 10 §4.5 collection envelope with cursor pagination, each element carrying the offer's price
# and timing, a closed customer-facing provider summary and the vehicle it is offered with; a
# provider gets what a stranger gets; the response carries no budget in any form, and no provider's
# service area, specialties or other jobs."
#
# The read SHIP-102's comparison screen had nothing to work from without. Docs/11 §6 struck that
# ticket for two waves as "every dependency met and unbuildable in fact" — all four things it names
# were unserved, because the only bidding reads on the surface were a provider's own bids and one
# negotiation's history, and the history needs a bid identifier the customer would have to hold.
#
# # The path is five segments and Docs/09 says four
#
# `GET /v1/jobs/{id}/bids` could not be registered when this section was written: `GET
# /v1/jobs/open/{id}` put a literal where the job identifier goes, so both patterns matched
# `/v1/jobs/open/bids` with neither more specific and the mux refused the pair at start-up.
# **SHIP-83a has since moved the feed to `/v1/fleet/jobs/{id}` and the four-segment space is free**
# — cmd/api/routes_jobsegment_test.go demonstrates it by registering one. The path here stays at
# five segments because it is already published and moving it would break every installed client
# for a tidier URL, which is not a trade `Docs/06` §5.3 permits.
#
# # What only this can show
#
# internal/bidding/offers_test.go holds the ownership rule, the closed key set, the word guard and
# the keyset. What only this can show is the route being served at all, at five segments, past the
# real auth class, in one running router — and against offers this file placed through real
# endpoints on a job that really carries a budget.

# A job of its own, so that the assertions below are about offers this section placed rather than
# about whatever state the sections above left the shared job in. It carries the same budget, which
# is what makes the privacy checks real.
status="$(bid_post "$bid_customer_token" "verify-offers-job-$$" /v1/jobs \
  '{"pickup":{"line":"5 Church Street","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"1 Bourke Street","suburb":"Melbourne","state":"VIC","postcode":"3000"},
    "goods_description":"Two-seater sofa","weight_kg":80,"length_cm":190,"width_cm":90,"height_cm":80,
    "budget_cents":432199}' offers-job)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-offers-job.json"; fail "creating the offers job returned $status"; }
offers_job_id="$(json "$WORKDIR/bid-offers-job.json" '["id"]')"
move_job "$offers_job_id" Draft Open

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from jobs where id = '$offers_job_id' and budget is not null;")" == "1" ]] \
  || fail "the offers job carries no budget, so every privacy check in this section would be vacuous"

# The provider's own vehicle, named on the offer — the column 000504 added and the clause of the
# *Done when* that could not be served before it. Read out of the fleet endpoint rather than the
# database, so that what the offer names is what a client would have had to hand.
status="$(curl -s -o "$WORKDIR/bid-offers-fleet.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $bid_provider_token" "http://localhost:$VERIFY_PORT/v1/fleet/vehicles")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-offers-fleet.json"; fail "reading the provider's fleet returned $status"; }
offers_vehicle_id="$(python3 -c '
import json, sys
print(json.load(open(sys.argv[1]))["data"][0]["id"])
' "$WORKDIR/bid-offers-fleet.json")"
[[ "$offers_vehicle_id" =~ ^[0-9a-f-]{36}$ ]] || fail "could not read a vehicle out of the provider fleet"

offers_body="{\"amount_cents\":45000,\"pickup_at\":\"$bid_pickup\",\"deliver_by\":\"$bid_deliver\",\"vehicle_id\":\"$offers_vehicle_id\"}"
status="$(bid_post "$bid_provider_token" "verify-offers-a-$$" "/v1/jobs/$offers_job_id/bids" "$offers_body" offers-a)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-offers-a.json"; fail "the first offer returned $status"; }
offers_bid_a="$(json "$WORKDIR/bid-offers-a.json" '["id"]')"

# **A vehicle that is not the caller's, and it goes here rather than later for a reason the Go
# tests found first.** The refusal has to be a *field* error — 422 naming `vehicle_id`, which sends a
# client back to their own fleet list — and it is only reachable while the caller is otherwise able
# to bid. Eligibility is checked before the vehicle is, so this attempt has to be made on a job the
# rival can still bid on and before they have a live offer on it: run against the section's older
# job it answers 404 about the job, and run after their own offer it answers 409, and neither says
# anything about the vehicle.
status="$(bid_post "$bid_rival_token" "verify-offers-steal-$$" "/v1/jobs/$offers_job_id/bids" \
  "{\"amount_cents\":41000,\"pickup_at\":\"$bid_pickup\",\"deliver_by\":\"$bid_deliver\",\"vehicle_id\":\"$offers_vehicle_id\"}" offers-steal)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/bid-offers-steal.json"; fail "offering another provider's vehicle returned $status, want 422"; }
[[ "$(json "$WORKDIR/bid-offers-steal.json" '["error"]["details"][0]["field"]')" == "vehicle_id" ]] \
  || fail "the refusal does not name vehicle_id: $(cat "$WORKDIR/bid-offers-steal.json")"
ok "an offer names a vehicle from the caller's own fleet, and another provider's is refused on the field"

# The rival offers without naming a vehicle, which is the other half of the pair: `vehicle` is
# omitted rather than sent empty, and a client can tell the two apart without a second flag.
status="$(bid_post "$bid_rival_token" "verify-offers-b-$$" "/v1/jobs/$offers_job_id/bids" \
  "{\"amount_cents\":39900,\"pickup_at\":\"$bid_pickup\",\"deliver_by\":\"$bid_deliver\"}" offers-b)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-offers-b.json"; fail "the second offer returned $status"; }
offers_bid_b="$(json "$WORKDIR/bid-offers-b.json" '["id"]')"

offers_get() {
  curl -s -o "$WORKDIR/bid-offers-$3.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" \
    "http://localhost:$VERIFY_PORT/v1/jobs/$offers_job_id/bids/received$2"
}

status="$(curl -s -o "$WORKDIR/bid-offers-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$offers_job_id/bids/received")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/bid-offers-anon.json"; fail "an unauthenticated read returned $status, want 401"; }
ok "it cannot be reached without a credential"

status="$(offers_get "$bid_customer_token" "" list)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-offers-list.json"; fail "the owning customer got $status"; }

python3 - "$WORKDIR/bid-offers-list.json" "$offers_bid_a" "$offers_bid_b" "$offers_vehicle_id" <<'PY' \
  || fail "the offers list is not what SHIP-102a's Done when describes"
import json, sys

page = json.load(open(sys.argv[1]))
wanted_a, wanted_b, vehicle = sys.argv[2], sys.argv[3], sys.argv[4]

if sorted(page) not in (["data", "has_more"], ["data", "has_more", "next_cursor"]):
    sys.exit("the envelope carries %s" % sorted(page))

rows = {row["id"]: row for row in page["data"]}
if set(rows) != {wanted_a, wanted_b}:
    sys.exit("the customer sees %s, want both offers on their job" % sorted(rows))

for row in rows.values():
    # Price and timing — the first two things Docs/01 4.3 asks a customer to compare.
    for field in ("amount_cents", "pickup_at", "deliver_by", "status", "offered_by"):
        if not row.get(field):
            sys.exit("an offer carries no %s" % field)
    # The provider summary, on every element and closed.
    summary = row.get("provider")
    if sorted(summary or {}) != ["id", "member_since", "verified"]:
        sys.exit("the provider summary carries %s" % sorted(summary or {}))
    if summary["verified"] is not True:
        sys.exit("a verified fixture provider is reported unverified")

# The vehicle, on the offer that named one and absent from the one that did not.
if rows[wanted_a].get("vehicle", {}).get("id") != vehicle:
    sys.exit("the offer that named a vehicle carries %r" % rows[wanted_a].get("vehicle"))
if sorted(rows[wanted_a]["vehicle"]["capacity"]) != ["height_cm", "length_cm", "max_weight_kg", "width_cm"]:
    sys.exit("the declared capability carries %s" % sorted(rows[wanted_a]["vehicle"]["capacity"]))
if "registration" in rows[wanted_a]["vehicle"]:
    sys.exit("the vehicle carries its registration, which a losing bidder never published to this customer")
if "vehicle" in rows[wanted_b]:
    sys.exit("an offer that named no vehicle carries one")
PY
ok "the owning customer compares price, timing, a closed provider summary and the vehicle, side by side"

# **The clause that needs its own check, in both directions and byte for byte.** A refusal that
# differed by a word between "not yours" and "no such job" would still confirm the job exists.
absent_job="$(python3 -c 'import uuid; print(uuid.uuid4())')"
status="$(offers_get "$bid_provider_token" "" refused-provider)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/bid-offers-refused-provider.json"; fail "the bidding provider got $status, want 404"; }
status="$(offers_get "$bid_rival_token" "" refused-rival)"
[[ "$status" == "404" ]] || fail "the rival provider got $status, want 404"
status="$(curl -s -o "$WORKDIR/bid-offers-refused-absent.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $bid_customer_token" \
  "http://localhost:$VERIFY_PORT/v1/jobs/$absent_job/bids/received")"
[[ "$status" == "404" ]] || fail "a job that does not exist got $status, want 404"

python3 - "$WORKDIR/bid-offers-refused-provider.json" "$WORKDIR/bid-offers-refused-rival.json" \
  "$WORKDIR/bid-offers-refused-absent.json" <<'PY' || fail "the three refusals are not the same answer"
import json, sys

# `request_id` is dropped by name rather than pattern-matched away, and that is the correction this
# check needed: it is a 32-character hex string with no hyphens, so a UUID regular expression does
# not touch it and three identical refusals compare as three different ones. Naming the field says
# what is actually per-request; a looser pattern would also erase a difference worth failing on.
#
# **Everything else is compared verbatim**, including the code and the message. A refusal that
# differed by a word between "not yours" and "no such job" would confirm the job exists, which is
# the disclosure this endpoint's 404 is for.
bodies = []
for path in sys.argv[1:]:
    body = json.load(open(path))
    if "amount_cents" in json.dumps(body):
        sys.exit("a refusal carried an offer: %s" % body)
    if not isinstance(body.get("error"), dict) or "request_id" not in body["error"]:
        sys.exit("a refusal carries no request id, so this check is comparing the wrong shape: %s" % body)
    body["error"].pop("request_id")
    bodies.append(json.dumps(body, sort_keys=True))

if len(set(bodies)) != 1:
    sys.exit("the refusals differ: %s" % bodies)
PY
ok "a provider bidding on the job, a rival, and a job that does not exist are told the same thing, byte for byte"

# The three subjects the *Done when* forbids, over the whole document. **The word check is the one
# wave 9's finding is about**: a response saying "the customer has set a maximum" carries no field
# and no amount, and passes a closed key set and a source scan alike.
python3 - "$WORKDIR/bid-offers-list.json" <<'PY' || fail "the offers response carries something it may not"
import json, re, sys

raw = open(sys.argv[1]).read()
document = json.loads(raw)

allowed = {
    "data", "next_cursor", "has_more",
    "id", "job_id", "status", "offered_by", "amount_cents", "pickup_at", "deliver_by",
    "message", "superseded_by", "created_at", "updated_at",
    "provider", "verified", "member_since",
    "vehicle", "type", "make", "model", "capacity",
    "max_weight_kg", "length_cm", "width_cm", "height_cm",
}

def walk(node):
    if isinstance(node, dict):
        for key, child in node.items():
            if key not in allowed:
                sys.exit("the customer's response carries %r" % key)
            walk(child)
    elif isinstance(node, list):
        for child in node:
            walk(child)

walk(document)

searchable = re.sub(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}", "<id>", raw)
for rendering in ("4321.99", "432199", "4321,99", "4,321.99"):
    if rendering in searchable:
        sys.exit("the customer's budget appears as %r" % rendering)

lowered = raw.lower()
for word in ("budget", "maximum", "ceiling", "price cap", "service area", "specialt", "victoria"):
    if word in lowered:
        sys.exit("the response contains the word %r — a sentence saying a budget exists is the "
                 "flag Docs/01 4.3 forbids, and a provider's declared area is excluded by name" % word)
PY
ok "no budget in any form — no field, no value, and no sentence saying one exists — and no declaration of the provider's"

# Live by default, the rest by name. The comparison screen shows what can be awarded.
status="$(bid_post "$bid_rival_token" "verify-offers-wd-$$" \
  "/v1/jobs/$offers_job_id/bids/$offers_bid_b/withdraw" '{}' offers-withdraw)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-offers-withdraw.json"; fail "withdrawing returned $status"; }

status="$(offers_get "$bid_customer_token" "" live)"
[[ "$status" == "200" ]] || fail "the live list returned $status"
python3 - "$WORKDIR/bid-offers-live.json" "$offers_bid_a" <<'PY' || fail "a withdrawn offer is on the comparison screen"
import json, sys
rows = json.load(open(sys.argv[1]))["data"]
if [row["id"] for row in rows] != [sys.argv[2]]:
    sys.exit("the live list holds %s" % [row["id"] for row in rows])
PY
status="$(offers_get "$bid_customer_token" "?status=withdrawn" withdrawn)"
[[ "$status" == "200" ]] || fail "?status=withdrawn returned $status"
python3 - "$WORKDIR/bid-offers-withdrawn.json" "$offers_bid_b" <<'PY' || fail "?status= did not reach the withdrawn offer"
import json, sys
rows = json.load(open(sys.argv[1]))["data"]
if [row["id"] for row in rows] != [sys.argv[2]]:
    sys.exit("the withdrawn group holds %s" % [row["id"] for row in rows])
PY
ok "the default is the offers that can be awarded, and ?status= reaches the rest"

# Cursor pagination, and the two refusals a query parameter makes.
status="$(offers_get "$bid_customer_token" "?status=submitted&limit=1" page1)"
[[ "$status" == "200" ]] || fail "the first page returned $status"
python3 - "$WORKDIR/bid-offers-page1.json" <<'PY' || fail "a single-row page is not the envelope"
import json, sys
page = json.load(open(sys.argv[1]))
if len(page["data"]) != 1:
    sys.exit("limit=1 returned %d rows" % len(page["data"]))
if page["has_more"] or page.get("next_cursor"):
    sys.exit("one live offer reported another page: %s" % page)
PY
status="$(offers_get "$bid_customer_token" "?cursor=not-a-cursor" badcursor)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/bid-offers-badcursor.json"; fail "a mangled cursor returned $status, want 400"; }
status="$(offers_get "$bid_customer_token" "?status=haggling" badstatus)"
[[ "$status" == "400" ]] || fail "an unknown status returned $status, want 400"
ok "cursor pagination in the Docs/10 §4.5 envelope, and a cursor this endpoint did not issue is refused rather than read as the first page"

# ---------------------------------------------------------------------------------------
ticket "SHIP-79a  a provider profile a customer may be shown — a closed set, and none of it the service area"

# **What only the harness can show.** `internal/fleet` owns the declaration and has no
# customer-facing endpoint; `internal/bidding` owns this response and may not import `fleet`; the two
# meet in cmd/api's offeror directory. A domain test can assert either half against a fake. Only a
# running service proves the seam — that what a provider typed into PATCH /v1/fleet/profile is what
# the customer's comparison screen reads back, through the real wiring.

status="$(curl -s -X PATCH -o "$WORKDIR/profile-79a.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $bid_provider_token" -H "Idempotency-Key: verify-79a-declare-$$" \
  -H 'Content-Type: application/json' \
  -d '{"display_name":"Yarra Valley Freight","operates_as":"business","specialties":["refrigerated"],"service_area":{"postcodes":["3121"]}}' \
  "http://localhost:$VERIFY_PORT/v1/fleet/profile")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/profile-79a.json"; fail "declaring the public profile returned $status"; }

# The declaration has a service area and a specialty to leak, or the assertions below are vacuous —
# the same fixture discipline the budget checks follow.
python3 - "$WORKDIR/profile-79a.json" <<'DECLARED' || fail "the provider declared no area or specialty, so the disclosure checks prove nothing"
import json, sys
p = json.load(open(sys.argv[1]))
if not p["service_area"]["postcodes"] or not p["specialties"]:
    sys.exit("the declaration is empty: %s" % p)
if p.get("display_name") != "Yarra Valley Freight" or p.get("operates_as") != "business":
    sys.exit("the declaration did not survive: %s" % p)
DECLARED

status="$(offers_get "$bid_customer_token" "" seventy-nine-a)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/bid-offers-seventy-nine-a.json"; fail "listing the offers returned $status"; }

python3 - "$WORKDIR/bid-offers-seventy-nine-a.json" <<'SUMMARY' || fail "the customer's provider summary is wrong"
import json, sys

page = json.load(open(sys.argv[1]))
rows = page["data"]
if not rows:
    sys.exit("no offers, so this proves nothing")

# The closed set, at the provider summary. Anything outside it is a field somebody added without
# deciding what a customer may see.
permitted = {"id", "display_name", "operates_as", "verified", "member_since"}

described = 0
for row in rows:
    provider = row["provider"]
    extra = set(provider) - permitted
    if extra:
        sys.exit("the provider summary carries %s, which is not in the closed set" % sorted(extra))
    if provider.get("display_name"):
        described += 1

if described == 0:
    sys.exit("no offer names who is offering, which is the whole of SHIP-79a: %s" % rows)
SUMMARY

# And none of it is the declaration a competitor could use. The area and the specialty were declared
# above through the real endpoint, so this is a search for values that genuinely exist.
for disclosure in 3121 refrigerated service_area specialt postcode; do
  grep -qi -- "$disclosure" "$WORKDIR/bid-offers-seventy-nine-a.json" \
    && { cat "$WORKDIR/bid-offers-seventy-nine-a.json"; fail "the customer's offers carry '$disclosure' — SHIP-102a forbids the provider's service area and specialties"; }
done
ok "the customer is told who is offering, in a closed set, and none of it is the provider's service area or specialties"

unset offers_job_id offers_vehicle_id offers_body offers_bid_a offers_bid_b absent_job
unset -f offers_get
