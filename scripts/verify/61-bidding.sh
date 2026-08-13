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
# Nothing here reads Kafka or the outbox, so no fence is needed: placing a bid emits no event
# (SHIP-136 owns bidding's events) and moves no job.

# --- the accounts and the job these checks run against -----------------------------------------

status="$(post_json "verify-bid-prov-$$" /v1/auth/register \
  "{\"email\":\"bid-provider-$$@example.com\",\"phone\":\"04145$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/bid-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-provider.json"; fail "could not register the bidding provider: $status"; }
bid_provider_id="$(json "$WORKDIR/bid-provider.json" '["id"]')"
bid_provider_token="$(mint_token "$bid_provider_id")"

status="$(post_json "verify-bid-rival-$$" /v1/auth/register \
  "{\"email\":\"bid-rival-$$@example.com\",\"phone\":\"04146$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/bid-rival.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/bid-rival.json"; fail "could not register the rival provider: $status"; }
bid_rival_id="$(json "$WORKDIR/bid-rival.json" '["id"]')"
bid_rival_token="$(mint_token "$bid_rival_id")"

status="$(post_json "verify-bid-cust-$$" /v1/auth/register \
  "{\"email\":\"bid-customer-$$@example.com\",\"phone\":\"04147$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
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
move_job() {
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
"$PSQL" "$DATABASE_URL" -q -c \
  "update users set email_verified_at = now(), phone_verified_at = now()
     where id in ('$bid_provider_id', '$bid_rival_id');"

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
    "id", "job_id", "status", "amount_cents",
    "pickup_at", "deliver_by", "message",
    "created_at", "updated_at",
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
# leak because there is no job in the shape. GET /v1/jobs/open/{id} is where a provider reads the job.
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
move_job "$bid_job_id" Open Cancelled
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
ticket "SHIP-84  Docs/02 §1 makes two statuses biddable, and both are exercised"

# **Negotiating remains open to eligible bids.** Docs/02 §1: "'Negotiating' is a useful presentation
# status. Technically, the job remains available for eligible bids unless the customer closes it or
# awards a bid."
#
# Nothing can reach Negotiating until SHIP-90, so a filter accepting only Open would pass every other
# check in this file and surface months from now as jobs silently refusing bids the moment somebody
# negotiated. The status is set here directly because no endpoint can yet.
move_job "$retry_job" Open Negotiating

status="$(bid_post "$bid_rival_token" "verify-bid-negotiating-$$" "/v1/jobs/$retry_job/bids" "$bid_body" negotiating)"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/bid-negotiating.json"; fail "a Negotiating job refused a bid ($status). Docs/02 §1 keeps it open to eligible bids."; }
ok "a job at Negotiating still accepts a bid from an eligible provider"

# ---------------------------------------------------------------------------------------
ticket "SHIP-84  placing a bid does not move the job — Open → Negotiating is SHIP-90's"

# $second_job has carried two live offers since the privacy section above. A job with active bids
# presents as Negotiating in Docs/02 §1, and moving it there is **SHIP-90's** ticket, which depends on
# SHIP-87 and SHIP-57. It is deliberately not done here: a bid is not a status transition, so it
# leaves no job_status_history row either — and the history count is the half that would catch a move
# made through some other path.
after="$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status || ' ' || (select count(*) from bids where job_id = j.id)::text || ' '
          || (select count(*) from job_status_history where job_id = j.id)::text
     from jobs j where j.id = '$second_job';")"
[[ "$after" == "Open 2 1" ]] \
  || fail "the job reads '$after' (status, bids, history rows), want 'Open 2 1' — two offers must leave it Open with only its Draft → Open row"
ok "a job with two live offers is still Open, with no history row beyond the one that published it"

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
    "id", "job_id", "status", "amount_cents",
    "pickup_at", "deliver_by", "message",
    "created_at", "updated_at",
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
    "id", "job_id", "status", "amount_cents",
    "pickup_at", "deliver_by", "message",
    "created_at", "updated_at",
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

# Neither verb is a job transition. Docs/02 §2 has `Negotiating → Open` on bids being withdrawn, and
# that is SHIP-90's in both directions; the history count is the half that would catch a move made
# through some other path.
after="$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status || ' ' || (select count(*) from job_status_history where job_id = j.id)::text
     from jobs j where j.id = '$revise_job';")"
[[ "$after" == "Open 1" ]] \
  || fail "the job reads '$after' (status, history rows), want 'Open 1' — revising and withdrawing move no job"
ok "the job is still Open with only the row that published it — Negotiating is SHIP-90's, both ways"
