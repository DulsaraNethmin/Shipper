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
