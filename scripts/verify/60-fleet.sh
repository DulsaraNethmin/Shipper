# shellcheck shell=bash
#
# M3 fleet — the vehicles a provider maintains, and the rules a client cannot see.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 60–69 is the fleet and bidding range. This file is fleet's to append to, and no other track's to
# edit — which is the whole reason the script was split (Docs/11 §9, SHIP-15e).
#
# The sections here are ordered rather than independent: the accounts and the tokens are made once
# at the top and read by everything after. They are made through the API rather than reused from
# 40-identity.sh or 50-jobs.sh, on the runner's own advice — a section that needs its own account
# registers one.
#
# # The tokens are minted, and every one of them claims `role: customer`
#
# mint_token is in the runner and signs with the development key from deploy/.env.example. It puts
# `role: customer` in every token it issues, which is exactly the thing worth exercising here: the
# platform decides what a caller may do by reading users.role, not by believing the claim. So the
# provider below succeeds with a token that says customer, and the customer is refused with an
# identical one — which is a stronger demonstration than two honest tokens would be.
#
# It also means no check here signs in, so SHIP-47's per-address bucket (rl:v1:signin:*, shared
# across sections and across runs from 127.0.0.1) is untouched by this file.

# --- the accounts these checks run as ----------------------------------------------------------

status="$(post_json "verify-fleet-prov-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"fleet-provider-$$@example.com\",\"phone\":\"04140$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/fleet-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/fleet-provider.json"; fail "could not register the fleet provider: $status"; }
fleet_provider_id="$(json "$WORKDIR/fleet-provider.json" '["id"]')"
fleet_provider_token="$(mint_token "$fleet_provider_id")"

status="$(post_json "verify-fleet-other-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"fleet-other-$$@example.com\",\"phone\":\"04141$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/fleet-other.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/fleet-other.json"; fail "could not register the second provider: $status"; }
fleet_other_id="$(json "$WORKDIR/fleet-other.json" '["id"]')"
fleet_other_token="$(mint_token "$fleet_other_id")"

status="$(post_json "verify-fleet-cust-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"fleet-customer-$$@example.com\",\"phone\":\"04142$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/fleet-customer.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/fleet-customer.json"; fail "could not register the fleet customer: $status"; }
fleet_customer_token="$(mint_token "$(json "$WORKDIR/fleet-customer.json" '["id"]')")"

# fleet_request <method> <token> <key> <path> <body> <outfile> — one authenticated state-changing
# request, answering with its status.
#
# Every writing route here is protected and state-changing, so each call needs a credential *and* an
# idempotency key. Passing both as arguments means a check that forgets one gets a wrong number of
# arguments rather than a 400 or a 401 that has nothing to do with what it was testing.
#
# The body may be empty, which is how deactivate and reactivate are called: those two take no body
# at all, and sending `{}` would demonstrate something other than what the contract says.
fleet_request() {
  if [[ -z "$5" ]]; then
    curl -s -X "$1" -o "$6" -w '%{http_code}' \
      -H "$auth_header: Bearer $2" -H "Idempotency-Key: $3" \
      "http://localhost:$VERIFY_PORT$4"
  else
    curl -s -X "$1" -o "$6" -w '%{http_code}' \
      -H "$auth_header: Bearer $2" -H "Idempotency-Key: $3" \
      -H 'Content-Type: application/json' -d "$5" "http://localhost:$VERIFY_PORT$4"
  fi
}

# fleet_get <token> <path> <outfile> — one authenticated read.
#
# No Idempotency-Key, and that is the point of a separate helper: a GET changes nothing, the
# middleware lets read-only methods through untouched, and a check that sent a key anyway would be
# demonstrating something other than what it claims.
fleet_get() {
  curl -s -o "$3" -w '%{http_code}' -H "$auth_header: Bearer $1" \
    "http://localhost:$VERIFY_PORT$2"
}

# ---------------------------------------------------------------------------------------
ticket "SHIP-78  POST /v1/fleet/vehicles adds a vehicle to the calling provider's fleet"

status="$(curl -s -X POST -o "$WORKDIR/fleet-anon.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-fleet-anon-$$" -H 'Content-Type: application/json' \
  -d '{"registration":"AAA111","vehicle_type":"van"}' \
  "http://localhost:$VERIFY_PORT/v1/fleet/vehicles")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/fleet-anon.json"; fail "an unauthenticated POST returned $status, want 401"; }
ok "it cannot be reached without a credential"

status="$(curl -s -X POST -o "$WORKDIR/fleet-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $fleet_provider_token" -H 'Content-Type: application/json' \
  -d '{"registration":"AAA111","vehicle_type":"van"}' \
  "http://localhost:$VERIFY_PORT/v1/fleet/vehicles")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/fleet-nokey.json"; fail "a request with no Idempotency-Key returned $status, want 400"; }
ok "and not without an Idempotency-Key — a retried phone must not end up with two vehicles"

status="$(fleet_request POST "$fleet_customer_token" "verify-fleet-cust-post-$$" /v1/fleet/vehicles \
  '{"registration":"CUS111","vehicle_type":"ute"}' "$WORKDIR/fleet-customer-post.json")"
[[ "$status" == "403" ]] || { cat "$WORKDIR/fleet-customer-post.json"; fail "a customer kept a fleet: $status"; }
[[ "$(json "$WORKDIR/fleet-customer-post.json" '["error"]["code"]')" == "fleet_provider_only" ]] \
  || { cat "$WORKDIR/fleet-customer-post.json"; fail "expected code=fleet_provider_only"; }
ok "a customer is refused, with a code the app can act on — and both tokens claim 'customer'"

# The plate is written the way it appears on the vehicle, with a space, and the type in the wrong
# case. Both are normalised, and the normalisation is not cosmetic: it is what stops one truck being
# recorded twice under two spellings.
vehicle_body='{
  "registration": "  abc 123 ",
  "vehicle_type": "Van",
  "make": "Mercedes-Benz",
  "model": "Sprinter  314 CDI",
  "max_weight_kg": 1200.5,
  "load_length_cm": 320
}'

status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-add-$$" /v1/fleet/vehicles \
  "$vehicle_body" "$WORKDIR/vehicle.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/vehicle.json"; fail "POST /v1/fleet/vehicles returned $status, want 201"; }
vehicle_id="$(json "$WORKDIR/vehicle.json" '["id"]')"
[[ "$vehicle_id" =~ ^[0-9a-f-]{36}$ ]] || fail "the response carries no vehicle id: $vehicle_id"
[[ "$(json "$WORKDIR/vehicle.json" '["registration"]')" == "ABC123" ]] \
  || fail "the registration was not normalised: $(json "$WORKDIR/vehicle.json" '["registration"]')"
[[ "$(json "$WORKDIR/vehicle.json" '["vehicle_type"]')" == "van" ]] || fail "'Van' was not normalised"
[[ "$(json "$WORKDIR/vehicle.json" '["model"]')" == "Sprinter 314 CDI" ]] || fail "the model's whitespace was not collapsed"
[[ "$(json "$WORKDIR/vehicle.json" '["active"]')" == "True" ]] || fail "a new vehicle is not in service"
ok "a vehicle is added in service, with its plate upper case and its spaces removed"

# The response is one thing; the row is another. Ownership and service state are read from the
# columns rather than from the answer the endpoint gave about itself.
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select provider_id || ' ' || registration || ' ' || vehicle_type || ' ' || coalesce(deactivated_at::text, 'in-service')
     from vehicles where id = '$vehicle_id';")"
[[ "$stored" == "$fleet_provider_id ABC123 van in-service" ]] \
  || fail "the stored vehicle is '$stored', want '$fleet_provider_id ABC123 van in-service'"
ok "the row is owned by the calling provider and carries the normalised plate"

# A vehicle with nothing but a plate and a type is a complete request. Docs/01 §4.2's acceptance
# measure is that a provider can maintain a fleet, and one standing in a truck yard may not know the
# load height.
status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-minimal-$$" /v1/fleet/vehicles \
  '{"registration":"MIN111","vehicle_type":"ute"}' "$WORKDIR/fleet-minimal.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/fleet-minimal.json"; fail "a minimal vehicle returned $status, want 201"; }
grep -q 'load_height_cm' "$WORKDIR/fleet-minimal.json" && fail "an unstated capacity is in the response"
ok "a plate and a type are enough, and what was not stated is omitted rather than sent as zero"

status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-required-$$" /v1/fleet/vehicles \
  '{"make":"Isuzu"}' "$WORKDIR/fleet-required.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/fleet-required.json"; fail "a vehicle with no plate returned $status, want 422"; }
grep -q '"registration"' "$WORKDIR/fleet-required.json" || fail "no detail names registration"
grep -q '"vehicle_type"' "$WORKDIR/fleet-required.json" || fail "no detail names vehicle_type"
ok "a plate and a type are required, and both missing fields are named at once"

status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-badtype-$$" /v1/fleet/vehicles \
  '{"registration":"BAD111","vehicle_type":"hovercraft"}' "$WORKDIR/fleet-badtype.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/fleet-badtype.json"; fail "an unknown vehicle type returned $status, want 422"; }
[[ "$(json "$WORKDIR/fleet-badtype.json" '["error"]["code"]')" == "validation_failed" ]] \
  || { cat "$WORKDIR/fleet-badtype.json"; fail "expected code=validation_failed"; }
ok "a vehicle type outside the eleven is refused, naming the field"

# ---------------------------------------------------------------------------------------
ticket "SHIP-78  one live vehicle per plate, enforced by a partial unique index"

status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-dup-$$" /v1/fleet/vehicles \
  '{"registration":"ABC 123","vehicle_type":"ute"}' "$WORKDIR/fleet-dup.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/fleet-dup.json"; fail "a duplicate plate returned $status, want 409"; }
[[ "$(json "$WORKDIR/fleet-dup.json" '["error"]["code"]')" == "fleet_duplicate_registration" ]] \
  || { cat "$WORKDIR/fleet-dup.json"; fail "expected code=fleet_duplicate_registration"; }
ok "the same plate written differently is one truck, and the second is refused"

# Another provider may hold the same plate. That is a real-world dispute — a sold vehicle whose
# previous owner never deactivated it — and adjudicating it is verification's job (Docs/04) rather
# than a constraint's, which would refuse whichever of the two typed second.
status="$(fleet_request POST "$fleet_other_token" "verify-fleet-otherprov-$$" /v1/fleet/vehicles \
  '{"registration":"ABC123","vehicle_type":"van"}' "$WORKDIR/fleet-otherprov.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/fleet-otherprov.json"; fail "a second provider was refused a plate: $status"; }
ok "the rule is per provider — another provider may hold the same plate"

# ---------------------------------------------------------------------------------------
ticket "SHIP-78  PATCH /v1/fleet/vehicles/{id} edits a vehicle the caller owns"

status="$(fleet_request PATCH "$fleet_provider_token" "verify-fleet-edit-$$" "/v1/fleet/vehicles/$vehicle_id" \
  '{"vehicle_type":"box_truck","max_weight_kg":0,"load_width_cm":175}' "$WORKDIR/fleet-edited.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/fleet-edited.json"; fail "PATCH returned $status, want 200"; }
[[ "$(json "$WORKDIR/fleet-edited.json" '["vehicle_type"]')" == "box_truck" ]] || fail "the type was not updated"
[[ "$(json "$WORKDIR/fleet-edited.json" '["load_width_cm"]')" == "175" ]] || fail "the load width was not set"
ok "the owner's edit is applied"

# A field that was not mentioned is left alone, and one sent empty is cleared. Those are two
# different things and a client that could not express the second could never take back a capacity
# it had stated.
[[ "$(json "$WORKDIR/fleet-edited.json" '["make"]')" == "Mercedes-Benz" ]] \
  || fail "a field that was not mentioned changed: make"
grep -q '"max_weight_kg"' "$WORKDIR/fleet-edited.json" && fail "the cleared capacity is still in the response"
ok "absent means unchanged and empty means cleared, which are not the same request"

status="$(fleet_request PATCH "$fleet_provider_token" "verify-fleet-blank-$$" "/v1/fleet/vehicles/$vehicle_id" \
  '{"registration":"   "}' "$WORKDIR/fleet-blank.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/fleet-blank.json"; fail "blanking the plate returned $status, want 422"; }
ok "the plate and the type cannot be cleared — a row with neither describes nothing"

status="$(fleet_request PATCH "$fleet_provider_token" "verify-fleet-noop-$$" "/v1/fleet/vehicles/$vehicle_id" \
  '{}' "$WORKDIR/fleet-noop.json")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/fleet-noop.json"; fail "an edit naming no field returned $status, want 400"; }
ok "an edit that changes nothing is refused rather than answered with an unchanged vehicle"

# ---------------------------------------------------------------------------------------
ticket "SHIP-78  service state is not a settable field, through the API either"

for verb_and_target in "POST /v1/fleet/vehicles" "PATCH /v1/fleet/vehicles/$vehicle_id"; do
  verb="${verb_and_target%% *}"
  target="${verb_and_target##* }"

  status="$(fleet_request "$verb" "$fleet_provider_token" "verify-fleet-setactive-$verb-$$" \
    "$target" '{"registration":"SET111","vehicle_type":"van","active":false}' "$WORKDIR/fleet-setactive.json")"
  [[ "$status" == "400" ]] \
    || { cat "$WORKDIR/fleet-setactive.json"; fail "$verb accepted an active field: $status"; }
  grep -q 'active' "$WORKDIR/fleet-setactive.json" || fail "the refusal does not name the field"
done
ok "neither endpoint accepts an 'active' field; the unknown field is reported, not ignored"

status="$(fleet_request PATCH "$fleet_provider_token" "verify-fleet-setdeact-$$" "/v1/fleet/vehicles/$vehicle_id" \
  '{"deactivated_at":"2026-08-11T03:30:00Z"}' "$WORKDIR/fleet-setdeact.json")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/fleet-setdeact.json"; fail "PATCH accepted deactivated_at: $status"; }
ok "nor a deactivated_at — taking a vehicle off the road is an intent, not a field"

# ---------------------------------------------------------------------------------------
ticket "SHIP-78  deactivate and reactivate, and the row survives both"

status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-deact-$$" \
  "/v1/fleet/vehicles/$vehicle_id/deactivate" '' "$WORKDIR/fleet-deactivated.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/fleet-deactivated.json"; fail "deactivating returned $status, want 200"; }
[[ "$(json "$WORKDIR/fleet-deactivated.json" '["active"]')" == "False" ]] || fail "the vehicle is still in service"
[[ -n "$(json "$WORKDIR/fleet-deactivated.json" '["deactivated_at"]')" ]] || fail "no deactivated_at was returned"
ok "the vehicle leaves service, and the request carried no body at all"

# The row, not the answer the endpoint gave about itself. This is the difference between
# deactivating and deleting, and it is the reason there is no DELETE: a vehicle is named by the bid
# that won a job and by the delivery that followed it (Docs/05 §3.1).
deactivated_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) || ' ' || coalesce(max(deactivated_at) is not null, false)::text
     from vehicles where id = '$vehicle_id';")"
[[ "$deactivated_row" == "1 true" ]] \
  || fail "the deactivated row is '$deactivated_row', want '1 true' — the row must survive"
ok "the row survives with its timestamp; nothing in this domain deletes a vehicle"

# Again, with a different key so the idempotency middleware is not what absorbs it.
first_deactivation="$(json "$WORKDIR/fleet-deactivated.json" '["deactivated_at"]')"
status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-deact-again-$$" \
  "/v1/fleet/vehicles/$vehicle_id/deactivate" '' "$WORKDIR/fleet-deact-again.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/fleet-deact-again.json"; fail "deactivating twice returned $status, want 200"; }
[[ "$(json "$WORKDIR/fleet-deact-again.json" '["deactivated_at"]')" == "$first_deactivation" ]] \
  || fail "the second deactivation moved the timestamp"
ok "a second deactivation with a fresh key is absorbed, and records nothing further"

# A deactivated vehicle can still be edited, and editing it does not bring it back. Refusing the
# edit would push a provider towards a second row for the same truck.
status="$(fleet_request PATCH "$fleet_provider_token" "verify-fleet-editoff-$$" "/v1/fleet/vehicles/$vehicle_id" \
  '{"model":"NPR 45-155"}' "$WORKDIR/fleet-editoff.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/fleet-editoff.json"; fail "editing a retired vehicle returned $status, want 200"; }
[[ "$(json "$WORKDIR/fleet-editoff.json" '["active"]')" == "False" ]] \
  || fail "an edit brought the vehicle back into service"
ok "a retired vehicle can still be corrected, and correcting it does not return it to service"

status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-react-$$" \
  "/v1/fleet/vehicles/$vehicle_id/reactivate" '' "$WORKDIR/fleet-reactivated.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/fleet-reactivated.json"; fail "reactivating returned $status, want 200"; }
[[ "$(json "$WORKDIR/fleet-reactivated.json" '["active"]')" == "True" ]] || fail "the vehicle did not return to service"
grep -q 'deactivated_at' "$WORKDIR/fleet-reactivated.json" && fail "a vehicle in service still reports deactivated_at"
ok "and comes back while nothing else holds its plate"

# The one collision only reactivation can hit: a replacement was added on the plate while this
# vehicle was out of service, and the partial unique index refuses the second live row.
status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-deact-two-$$" \
  "/v1/fleet/vehicles/$vehicle_id/deactivate" '' "$WORKDIR/fleet-deact-two.json")"
[[ "$status" == "200" ]] || fail "could not deactivate for the replacement case: $status"
status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-replacement-$$" /v1/fleet/vehicles \
  '{"registration":"ABC123","vehicle_type":"van"}' "$WORKDIR/fleet-replacement.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/fleet-replacement.json"; fail "a retired plate could not be used again: $status"; }
ok "a retired plate can be given to a replacement vehicle"

status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-react-clash-$$" \
  "/v1/fleet/vehicles/$vehicle_id/reactivate" '' "$WORKDIR/fleet-react-clash.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/fleet-react-clash.json"; fail "reactivating onto a taken plate returned $status, want 409"; }
[[ "$(json "$WORKDIR/fleet-react-clash.json" '["error"]["code"]')" == "fleet_duplicate_registration" ]] \
  || { cat "$WORKDIR/fleet-react-clash.json"; fail "expected code=fleet_duplicate_registration"; }
ok "and cannot come back while the replacement holds it — the index decides, not the application"

# ---------------------------------------------------------------------------------------
ticket "SHIP-78  GET /v1/fleet/vehicles/{id} is the owning provider's, and nobody else's"

status="$(fleet_get "$fleet_provider_token" "/v1/fleet/vehicles/$vehicle_id" "$WORKDIR/fleet-detail.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/fleet-detail.json"; fail "GET returned $status, want 200"; }
ok "the owner reads their own vehicle"

# One shape, whatever the client did to obtain it. A "detail" response with a field or two more
# would make every write response a subset a client has to special-case.
status="$(fleet_request POST "$fleet_provider_token" "verify-fleet-deact-idem-$$" \
  "/v1/fleet/vehicles/$vehicle_id/deactivate" '' "$WORKDIR/fleet-deact-idem.json")"
[[ "$status" == "200" ]] || fail "the absorbed deactivation returned $status"
python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))
b = json.load(open(sys.argv[2]))
sys.exit(0 if a == b else 1)
" "$WORKDIR/fleet-detail.json" "$WORKDIR/fleet-deact-idem.json" \
  || fail "the read and the write answer with different shapes"
ok "the same shape the writing endpoints answer with, so a client parses one type"

for suffix_and_verb in "GET:" "PATCH:" "POST:/deactivate" "POST:/reactivate"; do
  verb="${suffix_and_verb%%:*}"
  suffix="${suffix_and_verb#*:}"
  # The suffix carries a slash, which is legal in a header but noisy in a Redis key.
  key_part="${verb}${suffix//\//-}"
  body=''
  [[ "$verb" == "PATCH" ]] && body='{"make":"Hino"}'

  if [[ "$verb" == "GET" ]]; then
    theirs="$(fleet_get "$fleet_other_token" "/v1/fleet/vehicles/$vehicle_id" "$WORKDIR/fleet-theirs.json")"
    nothing="$(fleet_get "$fleet_other_token" "/v1/fleet/vehicles/00000000-0000-7000-8000-000000000010" "$WORKDIR/fleet-nothing.json")"
  else
    theirs="$(fleet_request "$verb" "$fleet_other_token" "verify-fleet-stranger-$key_part-$$" \
      "/v1/fleet/vehicles/$vehicle_id$suffix" "$body" "$WORKDIR/fleet-theirs.json")"
    nothing="$(fleet_request "$verb" "$fleet_other_token" "verify-fleet-nothing-$key_part-$$" \
      "/v1/fleet/vehicles/00000000-0000-7000-8000-000000000010$suffix" "$body" "$WORKDIR/fleet-nothing.json")"
  fi

  [[ "$theirs" == "404" && "$nothing" == "404" ]] \
    || { cat "$WORKDIR/fleet-theirs.json"; fail "$verb$suffix: somebody else's = $theirs, no such vehicle = $nothing; want 404 for both"; }
  python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$WORKDIR/fleet-theirs.json" "$WORKDIR/fleet-nothing.json" \
    || fail "$verb$suffix: another provider's vehicle answers differently from no vehicle at all"
done
ok "every route answers a stranger with the same 404 a missing vehicle gets"

# The refusals are refusals, not rollbacks that happened to work. Which vehicles a competitor runs
# is commercial information they never published.
still="$("$PSQL" "$DATABASE_URL" -tAc \
  "select make || ' ' || (deactivated_at is null)::text from vehicles where id = '$vehicle_id';")"
[[ "$still" == "Mercedes-Benz false" ]] || fail "the stranger reached the row: $still"
ok "nothing the stranger sent reached the row"

# ---------------------------------------------------------------------------------------
ticket "SHIP-78  GET /v1/fleet/vehicles lists the provider's own fleet, filtered and paginated"

status="$(fleet_get "$fleet_provider_token" /v1/fleet/vehicles "$WORKDIR/fleet-list.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/fleet-list.json"; fail "GET /v1/fleet/vehicles returned $status, want 200"; }

# Newest first, the envelope is Docs/10 §4.5's, and the list is the caller's alone. The second
# provider holds a vehicle on the same plate, so "only the caller's" has something to be wrong about
# rather than being a list that happens to be short.
python3 - "$WORKDIR/fleet-list.json" "$fleet_provider_id" <<'PY' || fail "the list is not the envelope Docs/10 §4.5 describes"
import json, sys
page = json.load(open(sys.argv[1]))
if set(page) - {"data", "next_cursor", "has_more"}:
    print("unexpected keys:", set(page), file=sys.stderr); sys.exit(1)
if not isinstance(page["data"], list) or "has_more" not in page:
    print("wrong shape:", page, file=sys.stderr); sys.exit(1)
created = [v["created_at"] for v in page["data"]]
if created != sorted(created, reverse=True):
    print("not newest first:", created, file=sys.stderr); sys.exit(1)
PY
ok "the envelope is data/next_cursor/has_more, newest first"

# The default carries retired vehicles too. A fleet screen that silently hid them would leave a
# provider unable to find the one they need to bring back.
python3 - "$WORKDIR/fleet-list.json" "$vehicle_id" <<'PY' || fail "the unfiltered list hides retired vehicles"
import json, sys
page = json.load(open(sys.argv[1]))
if not any(v["id"] == sys.argv[2] and not v["active"] for v in page["data"]):
    print("the retired vehicle is missing:", [(v["id"], v["active"]) for v in page["data"]], file=sys.stderr)
    sys.exit(1)
PY
ok "the default includes retired vehicles, which is what makes them findable"

status="$(fleet_get "$fleet_provider_token" '/v1/fleet/vehicles?active=true' "$WORKDIR/fleet-list-active.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/fleet-list-active.json"; fail "filtering returned $status"; }
python3 - "$WORKDIR/fleet-list-active.json" <<'PY' || fail "the active filter did not filter"
import json, sys
page = json.load(open(sys.argv[1]))
wrong = [v["registration"] for v in page["data"] if not v["active"]]
if wrong or not page["data"]:
    print("out of service and listed as active:", wrong or "the list is empty", file=sys.stderr)
    sys.exit(1)
PY
status="$(fleet_get "$fleet_provider_token" '/v1/fleet/vehicles?active=false' "$WORKDIR/fleet-list-retired.json")"
python3 - "$WORKDIR/fleet-list-retired.json" <<'PY' || fail "the retired filter did not filter"
import json, sys
page = json.load(open(sys.argv[1]))
wrong = [v["registration"] for v in page["data"] if v["active"]]
if wrong or not page["data"]:
    print("in service and listed as retired:", wrong or "the list is empty", file=sys.stderr)
    sys.exit(1)
PY
ok "?active= narrows the list in both directions"

# Refused rather than read as false, which would tell a client its filter worked.
status="$(fleet_get "$fleet_provider_token" '/v1/fleet/vehicles?active=yes' "$WORKDIR/fleet-list-badactive.json")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/fleet-list-badactive.json"; fail "?active=yes returned $status, want 400"; }
status="$(fleet_get "$fleet_provider_token" '/v1/fleet/vehicles?cursor=not-a-cursor' "$WORKDIR/fleet-list-badcursor.json")"
[[ "$status" == "400" ]] || fail "a mangled cursor returned $status, want 400"
status="$(fleet_get "$fleet_provider_token" '/v1/fleet/vehicles?limit=0' "$WORKDIR/fleet-list-badlimit.json")"
[[ "$status" == "400" ]] || fail "?limit=0 returned $status, want 400"
status="$(fleet_get "$fleet_provider_token" '/v1/fleet/vehicles?limit=5000' "$WORKDIR/fleet-list-biglimit.json")"
[[ "$status" == "200" ]] || fail "?limit=5000 returned $status, want it narrowed to the maximum"
ok "a nonsense filter, a mangled cursor and a bad limit are refused; an over-large limit is narrowed"

# Paging, followed the way a client follows it: take next_cursor, send it back, stop at
# has_more=false. The page size is 1 so the boundary is crossed several times.
python3 - "$fleet_provider_token" "$VERIFY_PORT" "$auth_header" <<'PY' || fail "paging did not reach every vehicle exactly once"
import json, sys, urllib.parse, urllib.request

token, port, header = sys.argv[1], sys.argv[2], sys.argv[3]
base = f"http://localhost:{port}/v1/fleet/vehicles"

def get(url):
    request = urllib.request.Request(url, headers={header: f"Bearer {token}"})
    with urllib.request.urlopen(request) as response:
        return json.load(response)

whole = get(f"{base}?limit=100")
if whole["has_more"]:
    print("the fixture has more than 100 vehicles; this check assumes it does not", file=sys.stderr)
    sys.exit(1)
expected = {v["id"] for v in whole["data"]}

seen, url, pages = [], f"{base}?limit=1", 0
while True:
    pages += 1
    if pages > len(expected) + 2:
        print("paging did not terminate; the cursor is not advancing", file=sys.stderr)
        sys.exit(1)
    page = get(url)
    seen.extend(v["id"] for v in page["data"])
    if not page["has_more"]:
        if page.get("next_cursor"):
            print("the last page carries a cursor", file=sys.stderr)
            sys.exit(1)
        break
    if len(page["data"]) != 1:
        print("a page before the last holds", len(page["data"]), "vehicles, want the limit of 1", file=sys.stderr)
        sys.exit(1)
    url = f"{base}?limit=1&cursor={urllib.parse.quote(page['next_cursor'])}"

if sorted(seen) != sorted(expected) or len(seen) != len(set(seen)):
    print("paged over", sorted(seen), "want", sorted(expected), file=sys.stderr)
    sys.exit(1)
print(f"    {len(expected)} vehicles over {pages} pages of one")
PY
ok "paging one vehicle at a time reaches every vehicle exactly once, and terminates"

# A provider with no fleet gets an empty array rather than null. A client iterating null breaks the
# first time a new provider opens the app, and never again in testing.
status="$(post_json "verify-fleet-fresh-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"fleet-fresh-$$@example.com\",\"phone\":\"04143$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/fleet-fresh.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/fleet-fresh.json"; fail "could not register a provider with no fleet"; }
status="$(fleet_get "$(mint_token "$(json "$WORKDIR/fleet-fresh.json" '["id"]')")" /v1/fleet/vehicles \
  "$WORKDIR/fleet-list-empty.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/fleet-list-empty.json"; fail "an empty fleet returned $status"; }
[[ "$(tr -d ' \n' < "$WORKDIR/fleet-list-empty.json")" == '{"data":[],"has_more":false}' ]] \
  || { cat "$WORKDIR/fleet-list-empty.json"; fail "an empty fleet is not an empty array"; }
ok "a provider with no fleet gets an empty array, never null"

# ---------------------------------------------------------------------------------------
ticket "SHIP-79  GET /v1/fleet/profile — a declaration nobody has made is empty, never missing"

status="$(curl -s -o "$WORKDIR/profile-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/fleet/profile")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/profile-anon.json"; fail "an unauthenticated GET returned $status, want 401"; }
ok "it cannot be reached without a credential"

# The provider registered at the top of this file has declared nothing yet, which is the state
# every provider is in when they first open the screen.
status="$(fleet_get "$fleet_provider_token" /v1/fleet/profile "$WORKDIR/profile-empty.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/profile-empty.json"; fail "GET /v1/fleet/profile returned $status, want 200"; }
[[ "$(tr -d ' \n' < "$WORKDIR/profile-empty.json")" == '{"service_area":{"states":[],"postcodes":[]},"specialties":[]}' ]] \
  || { cat "$WORKDIR/profile-empty.json"; fail "an undeclared profile is not three empty arrays"; }
ok "an undeclared profile is empty arrays rather than a 404 or a null"

# ---------------------------------------------------------------------------------------
ticket "SHIP-79  PATCH /v1/fleet/profile declares service area and specialties"

status="$(curl -s -X PATCH -o "$WORKDIR/profile-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $fleet_provider_token" -H 'Content-Type: application/json' \
  -d '{"specialties":["courier"]}' \
  "http://localhost:$VERIFY_PORT/v1/fleet/profile")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/profile-nokey.json"; fail "a request with no Idempotency-Key returned $status, want 400"; }
ok "and not without an Idempotency-Key — a retried phone must not declare twice"

status="$(fleet_request PATCH "$fleet_customer_token" "verify-profile-cust-$$" /v1/fleet/profile \
  '{"service_area":{"states":["NSW"]}}' "$WORKDIR/profile-customer.json")"
[[ "$status" == "403" ]] || { cat "$WORKDIR/profile-customer.json"; fail "a customer declared a service area: $status"; }
[[ "$(json "$WORKDIR/profile-customer.json" '["error"]["code"]')" == "fleet_provider_only" ]] \
  || { cat "$WORKDIR/profile-customer.json"; fail "expected code=fleet_provider_only"; }
ok "a customer is refused with a code the app can act on — and both tokens claim 'customer'"

# Everything below is written the way a person types it: a state spelled out, one in lower case,
# a postcode with a space in it, a specialty in title case, and each list carrying a repeat.
# Normalisation is what stops one region being recorded as two.
declaration='{
  "service_area": {"states": ["Victoria", "nsw", "VIC"], "postcodes": ["3 000", "0800", "3000"]},
  "specialties": ["Refrigerated", "general_freight", "refrigerated"]
}'

status="$(fleet_request PATCH "$fleet_provider_token" "verify-profile-declare-$$" /v1/fleet/profile \
  "$declaration" "$WORKDIR/profile.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/profile.json"; fail "PATCH /v1/fleet/profile returned $status, want 200"; }
python3 - "$WORKDIR/profile.json" <<'PY' || fail "the declaration was not normalised, deduplicated and ordered"
import json, sys
body = json.load(open(sys.argv[1]))
want = {
    "service_area": {"states": ["NSW", "VIC"], "postcodes": ["0800", "3000"]},
    "specialties": ["general_freight", "refrigerated"],
}
if body != want:
    print("got", body, "want", want, file=sys.stderr)
    sys.exit(1)
PY
ok "spelled-out states, mixed case and a spaced postcode become one ordered set each"

# The rows, not the answer the endpoint gave about itself. Two grains, one per row, and never
# both on one: a postcode is deliberately not validated against a state.
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select string_agg(scope || ':' || area, ',' order by scope, area)
     from provider_service_areas where provider_id = '$fleet_provider_id';")"
[[ "$stored" == "postcode:0800,postcode:3000,state:NSW,state:VIC" ]] \
  || fail "the stored service area is '$stored'"
ok "each entry is stored at one grain — a whole state, or one postcode, never both"

# ---------------------------------------------------------------------------------------
ticket "SHIP-79  a named list is replaced, an omitted one is left alone, an empty one is cleared"

status="$(fleet_request PATCH "$fleet_provider_token" "verify-profile-partial-$$" /v1/fleet/profile \
  '{"specialties":["courier"]}' "$WORKDIR/profile-partial.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/profile-partial.json"; fail "a partial declaration returned $status"; }
python3 - "$WORKDIR/profile-partial.json" <<'PY' || fail "naming only the specialties disturbed the service area"
import json, sys
body = json.load(open(sys.argv[1]))
if body["service_area"] != {"states": ["NSW", "VIC"], "postcodes": ["0800", "3000"]}:
    print("the service area changed:", body["service_area"], file=sys.stderr); sys.exit(1)
if body["specialties"] != ["courier"]:
    print("specialties =", body["specialties"], "want [courier] — a named list is replaced", file=sys.stderr)
    sys.exit(1)
PY
ok "an omitted list is unchanged, and a named one is replaced rather than merged into"

status="$(fleet_request PATCH "$fleet_provider_token" "verify-profile-clear-$$" /v1/fleet/profile \
  '{"service_area":{"postcodes":[]}}' "$WORKDIR/profile-cleared.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/profile-cleared.json"; fail "clearing the postcodes returned $status"; }
python3 - "$WORKDIR/profile-cleared.json" <<'PY' || fail "the empty list did not clear only the postcodes"
import json, sys
area = json.load(open(sys.argv[1]))["service_area"]
if area != {"states": ["NSW", "VIC"], "postcodes": []}:
    print("got", area, file=sys.stderr); sys.exit(1)
PY
ok "an empty list clears exactly that grain — the distinction a provider needs to withdraw"

status="$(fleet_request PATCH "$fleet_provider_token" "verify-profile-noop-$$" /v1/fleet/profile \
  '{}' "$WORKDIR/profile-noop.json")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/profile-noop.json"; fail "a declaration naming no list returned $status, want 400"; }
ok "a declaration that changes nothing is refused rather than answered with the unchanged one"

# ---------------------------------------------------------------------------------------
ticket "SHIP-79  a service area is a set of named regions, and a radius is not a field"

# The decision SHIP-79 took rather than leaving to SHIP-81: a job's coordinate is best-effort and
# its state and postcode are not, so eligibility is set membership. A client that believed
# otherwise is told the field does not exist rather than having its declaration quietly ignored.
for body in '{"service_area":{"radius_km":50}}' '{"service_area":{"latitude":-37.8,"longitude":144.9}}'; do
  status="$(fleet_request PATCH "$fleet_provider_token" "verify-profile-radius-$RANDOM-$$" \
    /v1/fleet/profile "$body" "$WORKDIR/profile-radius.json")"
  [[ "$status" == "400" ]] || { cat "$WORKDIR/profile-radius.json"; fail "$body returned $status, want 400"; }
done
ok "neither a radius nor a coordinate is accepted; the unknown field is reported, not ignored"

status="$(fleet_request PATCH "$fleet_provider_token" "verify-profile-invalid-$$" /v1/fleet/profile \
  '{"service_area":{"states":["NSW","Zealand"],"postcodes":["3000","12345"]},"specialties":["hovercraft"]}' \
  "$WORKDIR/profile-invalid.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/profile-invalid.json"; fail "an invalid declaration returned $status, want 422"; }
python3 - "$WORKDIR/profile-invalid.json" <<'PY' || fail "the refusal does not name each offending position"
import json, sys
details = json.load(open(sys.argv[1]))["error"]["details"]
named = {d["field"] for d in details}
want = {"service_area.states.1", "service_area.postcodes.1", "specialties.0"}
if not want <= named:
    print("named", named, "want at least", want, file=sys.stderr); sys.exit(1)
if "service_area.states.0" in named:
    print("a valid entry was reported:", named, file=sys.stderr); sys.exit(1)
PY
ok "every bad entry is named by its position, and the good ones are not"

# Nothing a refused declaration carried reached the database. A declaration is replaced whole, so
# a request that fails validation must leave the previous one exactly as it was.
still="$("$PSQL" "$DATABASE_URL" -tAc \
  "select string_agg(scope || ':' || area, ',' order by scope, area)
     from provider_service_areas where provider_id = '$fleet_provider_id';")"
[[ "$still" == "state:NSW,state:VIC" ]] || fail "the refused declaration reached the rows: '$still'"
ok "a refused declaration changes nothing that was already declared"

# The question SHIP-81 will ask, asked here in the shape it will ask it: set membership against a
# job's state and postcode, one index lookup, and no coordinate anywhere.
serves="$("$PSQL" "$DATABASE_URL" -tAc \
  "select exists (
     select 1 from provider_service_areas
      where provider_id = '$fleet_provider_id'
        and ((scope = 'state' and area = 'VIC') or (scope = 'postcode' and area = '3121'))
   ) || ' ' || exists (
     select 1 from provider_service_areas
      where provider_id = '$fleet_provider_id'
        and ((scope = 'state' and area = 'QLD') or (scope = 'postcode' and area = '4000'))
   );")"
[[ "$serves" == "true false" ]] || fail "the membership query answered '$serves', want 'true false'"
ok "the stored declaration answers eligibility by set membership — the query SHIP-81 inherits"

# ---------------------------------------------------------------------------------------
ticket "SHIP-81  the eligibility filter, through SHIP-82's GET /v1/fleet/jobs — four filters, each shown to exclude"

# **SHIP-81's SQL mirror is gone, and this is what replaced it.**
#
# SHIP-81 had no endpoint, so it demonstrated the four filters by running a hand-written copy of
# internal/fleet/eligibility.go's predicate against the real schema — clearly marked as a mirror,
# and with its own section header saying SHIP-82 would delete it. This is that deletion. Every
# check below now goes through the real endpoint, so there is one description of the rule rather
# than two, and a filter that stopped working would fail here instead of passing against a copy of
# itself.
#
# What the mirror bought — evidence from outside Go, against rows the real API created — is bought
# better this way: the request is the one a provider's phone makes.
#
# A provider of its own, deliberately. The sections above leave several vehicles in various states
# on $fleet_provider_id, and "no vehicle in service" cannot be demonstrated against a fleet whose
# contents this check does not control.

status="$(post_json "verify-elig-prov-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"fleet-eligible-$$@example.com\",\"phone\":\"04144$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/elig-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/elig-provider.json"; fail "could not register the eligibility provider: $status"; }
elig_provider_id="$(json "$WORKDIR/elig-provider.json" '["id"]')"
elig_provider_token="$(mint_token "$elig_provider_id")"

fleet_customer_id="$(json "$WORKDIR/fleet-customer.json" '["id"]')"

# Docs/04 §3's automated baseline, applied directly. Driving the real verification endpoints would
# need the token out of the console email and the OTP out of the log, which 40-identity.sh already
# demonstrates; repeating it here would be testing identity rather than eligibility.
"$PSQL" "$DATABASE_URL" -q -c \
  "update users set email_verified_at = now(), phone_verified_at = now() where id = '$elig_provider_id';"

status="$(fleet_request PATCH "$elig_provider_token" "verify-elig-profile-$$" /v1/fleet/profile \
  '{"service_area":{"states":["VIC"]}}' "$WORKDIR/elig-profile.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/elig-profile.json"; fail "declaring VIC returned $status"; }

status="$(fleet_request POST "$elig_provider_token" "verify-elig-vehicle-$$" /v1/fleet/vehicles \
  '{"registration":"ELG111","vehicle_type":"box_truck","max_weight_kg":1200,"load_length_cm":300,"load_width_cm":160,"load_height_cm":180}' \
  "$WORKDIR/elig-vehicle.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/elig-vehicle.json"; fail "adding the eligibility vehicle returned $status"; }
elig_vehicle_id="$(json "$WORKDIR/elig-vehicle.json" '["id"]')"

# The job carries a budget, deliberately, and it is the number SHIP-83's checks below search for.
# A job with no budget would make every one of those assertions vacuous — a privacy check whose
# fixture has nothing to leak passes forever and proves nothing.
status="$(fleet_request POST "$fleet_customer_token" "verify-elig-job-$$" /v1/jobs \
  '{"pickup":{"line":"5 Church Street","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"1 Bourke Street","suburb":"Melbourne","state":"VIC","postcode":"3000"},
    "goods_description":"Two-seater sofa","weight_kg":80,"length_cm":190,"width_cm":90,"height_cm":80,
    "budget_cents":150000}' \
  "$WORKDIR/elig-job.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/elig-job.json"; fail "creating the eligibility job returned $status"; }
elig_job_id="$(json "$WORKDIR/elig-job.json" '["id"]')"

# publish_job <job> <from> <to> — a status transition made the only way 000402 permits one: a
# job_status_history row written in the same transaction and named by shipper.job_status_transition.
# SHIP-63's publish endpoint does not exist yet, so the guard is satisfied directly rather than
# bypassed.
publish_job() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 <<SQL
DO \$do\$
DECLARE entry uuid := gen_random_uuid();
BEGIN
  INSERT INTO job_status_history
      (id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
  VALUES (entry, '$1', '$2', '$3', 'customer', '$fleet_customer_id', now());
  PERFORM set_config('shipper.job_status_transition', entry::text, true);
  UPDATE jobs SET status = '$3' WHERE id = '$1';
END
\$do\$;
SQL
}

publish_job "$elig_job_id" Draft Open

# eligible — 1 when the marketplace offers that one job to that one provider, 0 when it does not.
#
# **Both endpoints are asked, and they have to agree.** `GET /v1/fleet/jobs/{id}` answers 200 or 404,
# and the feed either carries the job or does not. One SQL predicate serves both, so a disagreement
# is a defect rather than a difference of emphasis — a provider shown a job in the feed and then
# refused it on the detail screen is the worst of both, and it is the failure sharing the clause
# exists to prevent. Every filter check below therefore exercises the two endpoints at once.
#
# Scoped to the single job by id, so the jobs 50-jobs.sh leaves behind cannot make a broken filter
# look like a working one.
eligible() {
  local detail feed status

  detail="$(fleet_get "$elig_provider_token" "/v1/fleet/jobs/$elig_job_id" "$WORKDIR/elig-detail.json")"
  case "$detail" in
    200) detail=1 ;;
    404) detail=0 ;;
    *) cat "$WORKDIR/elig-detail.json"; fail "GET /v1/fleet/jobs/$elig_job_id returned $detail, want 200 or 404" ;;
  esac

  status="$(fleet_get "$elig_provider_token" '/v1/fleet/jobs?limit=100' "$WORKDIR/elig-feed.json")"
  [[ "$status" == "200" ]] || { cat "$WORKDIR/elig-feed.json"; fail "GET /v1/fleet/jobs returned $status, want 200"; }
  feed="$(python3 - "$WORKDIR/elig-feed.json" "$elig_job_id" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
found = any(job["id"] == sys.argv[2] for job in page["data"])
if not found and page["has_more"]:
    # Ambiguous rather than false: the job could be on a later page, and a check that read
    # that as "excluded" would pass for the wrong reason on a busy database.
    print("more", file=sys.stderr)
    sys.exit(1)
print(1 if found else 0)
PY
)" || fail "the feed did not fit in one page of 100; this check cannot tell absent from further down"

  [[ "$detail" == "$feed" ]] || fail "the feed says $feed and GET /v1/fleet/jobs/$elig_job_id says $detail — one predicate serves both"
  printf '%s' "$detail"
}

status="$(curl -s -o "$WORKDIR/open-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/fleet/jobs")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/open-anon.json"; fail "an unauthenticated feed read returned $status, want 401"; }
ok "the feed cannot be reached without a credential — a provider sees it because the platform filtered it"

[[ "$(eligible)" == "1" ]] || fail "the job every filter should accept is not eligible"
ok "a verified provider serving VIC, with a truck that fits, is offered an Open Richmond job"

# SHIP-68 gives an Open job a deadline as it is published, and the filter reads it rather than
# trusting the status alone: the sweep runs on a ticker, so a job whose deadline has passed still
# says 'Open' until the worker reaches it.
deadline="$("$PSQL" "$DATABASE_URL" -tAc "select expires_at is not null from jobs where id = '$elig_job_id';")"
[[ "$deadline" == "t" ]] || fail "the published job carries no deadline"
"$PSQL" "$DATABASE_URL" -q -c "update jobs set expires_at = now() - interval '1 hour' where id = '$elig_job_id';"
[[ "$(eligible)" == "0" ]] || fail "a job past its deadline was still offered"
"$PSQL" "$DATABASE_URL" -q -c "update jobs set expires_at = now() + interval '14 days' where id = '$elig_job_id';"
ok "job status — a deadline that has passed excludes it before the sweep has reached it"

# Docs/04 §3's baseline, one channel at a time, then account standing. Restricted is excluded as
# well as suspended: §4 makes it "limited access pending clarification", and §1 says a provider does
# not bid until baseline checks are complete.
for column in email_verified_at phone_verified_at; do
  "$PSQL" "$DATABASE_URL" -q -c "update users set $column = null where id = '$elig_provider_id';"
  [[ "$(eligible)" == "0" ]] || fail "a provider with no $column was still offered work"
  "$PSQL" "$DATABASE_URL" -q -c "update users set $column = now() where id = '$elig_provider_id';"
done
for standing in restricted suspended; do
  "$PSQL" "$DATABASE_URL" -q -c "update users set status = '$standing' where id = '$elig_provider_id';"
  [[ "$(eligible)" == "0" ]] || fail "a $standing account was still offered work"
  "$PSQL" "$DATABASE_URL" -q -c "update users set status = 'active' where id = '$elig_provider_id';"
done
ok "verification state — an unverified, restricted or suspended provider is offered nothing"

# Through the API rather than by writing rows, because withdrawing from a region is something a
# provider actually does — and because an empty declaration matching nothing is SHIP-79's rule,
# which this is the enforcement of.
status="$(fleet_request PATCH "$elig_provider_token" "verify-elig-nsw-$$" /v1/fleet/profile \
  '{"service_area":{"states":["NSW"]}}' "$WORKDIR/elig-nsw.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/elig-nsw.json"; fail "redeclaring returned $status"; }
[[ "$(eligible)" == "0" ]] || fail "a provider who withdrew from VIC was still offered a VIC job"

status="$(fleet_request PATCH "$elig_provider_token" "verify-elig-none-$$" /v1/fleet/profile \
  '{"service_area":{"states":[],"postcodes":[]}}' "$WORKDIR/elig-none.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/elig-none.json"; fail "clearing the declaration returned $status"; }
[[ "$(eligible)" == "0" ]] || fail "a provider who has declared nothing was offered a job — eligibility is opt-in"
ok "service area — withdrawing excludes, and an empty declaration matches nothing, not everything"

status="$(fleet_request PATCH "$elig_provider_token" "verify-elig-vic-$$" /v1/fleet/profile \
  '{"service_area":{"states":["VIC"]}}' "$WORKDIR/elig-vic.json")"
[[ "$status" == "200" ]] || fail "restoring the declaration returned $status"

# Deactivation is what a provider does when a truck comes off the road, and Docs/01 §4.2's third
# verb. It must stop new work reaching them without touching work already under way.
status="$(fleet_request POST "$elig_provider_token" "verify-elig-off-$$" \
  "/v1/fleet/vehicles/$elig_vehicle_id/deactivate" '' "$WORKDIR/elig-off.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/elig-off.json"; fail "deactivating returned $status"; }
[[ "$(eligible)" == "0" ]] || fail "a provider whose only vehicle is off the road was still offered work"

status="$(fleet_request POST "$elig_provider_token" "verify-elig-on-$$" \
  "/v1/fleet/vehicles/$elig_vehicle_id/reactivate" '' "$WORKDIR/elig-on.json")"
[[ "$status" == "200" ]] || fail "reactivating returned $status"

# And a known mismatch excludes, while a missing measurement does not: 000300 lets a provider add a
# truck with a plate and nothing else, and 000404 lets a customer publish without measuring.
"$PSQL" "$DATABASE_URL" -q -c "update jobs set weight_kg = 9000 where id = '$elig_job_id';"
[[ "$(eligible)" == "0" ]] || fail "a nine-tonne load was offered to a 1200 kg truck"
"$PSQL" "$DATABASE_URL" -q -c "update jobs set weight_kg = null where id = '$elig_job_id';"
[[ "$(eligible)" == "1" ]] || fail "a job that states no weight was excluded; a missing measurement is not a mismatch"
"$PSQL" "$DATABASE_URL" -q -c "update jobs set weight_kg = 80 where id = '$elig_job_id';"
ok "vehicle capability — deactivation and a known mismatch exclude; an unstated measurement does not"

# Docs/02 §1: "'Negotiating' is a useful presentation status. Technically, the job remains
# available for eligible bids unless the customer closes it or awards a bid." Nothing can reach
# Negotiating until SHIP-90, which is exactly why this is asserted now.
publish_job "$elig_job_id" Open Negotiating
[[ "$(eligible)" == "1" ]] || fail "a Negotiating job was closed to new bids, which Docs/02 §1 does not do"
publish_job "$elig_job_id" Negotiating Cancelled
[[ "$(eligible)" == "0" ]] || fail "a cancelled job was still offered"
ok "job status — Negotiating stays biddable and Cancelled does not, exactly as Docs/02 §1 reads"

# ---------------------------------------------------------------------------------------
ticket "SHIP-82  GET /v1/fleet/jobs — the envelope, only eligible jobs, and paging"

# **A fresh job, because the one above is Cancelled and stays that way.** Docs/02 §1 makes
# Cancelled terminal, and moving it back would demonstrate a transition the platform does not
# offer — the database's guard only checks that a history row describes the move, so writing an
# illegal one here would be this script inventing a lifecycle rather than exercising one.
status="$(fleet_request POST "$fleet_customer_token" "verify-open-job-$$" /v1/jobs \
  '{"pickup":{"line":"5 Church Street","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"1 Bourke Street","suburb":"Melbourne","state":"VIC","postcode":"3000"},
    "goods_description":"Two-seater sofa","weight_kg":80,"length_cm":190,"width_cm":90,"height_cm":80,
    "vehicle_requirement":"Ute with a tailgate lifter","handling_notes":"Second-floor walk-up, no lift.",
    "budget_cents":150000}' \
  "$WORKDIR/open-job.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/open-job.json"; fail "creating the open-feed job returned $status"; }
open_job_id="$(json "$WORKDIR/open-job.json" '["id"]')"
publish_job "$open_job_id" Draft Open

# A job the provider is not eligible for, alongside one they are. "Only eligible jobs" needs
# something in the database to be wrong about; a feed filtered by an empty table proves nothing.
status="$(fleet_request POST "$fleet_customer_token" "verify-open-qld-$$" /v1/jobs \
  '{"pickup":{"line":"1 Queen Street","suburb":"Brisbane","state":"QLD","postcode":"4000"},
    "dropoff":{"line":"2 Adelaide Street","suburb":"Brisbane","state":"QLD","postcode":"4000"},
    "goods_description":"Pallet of tiles","weight_kg":300,
    "budget_cents":90000}' \
  "$WORKDIR/open-qld.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/open-qld.json"; fail "creating the out-of-area job returned $status"; }
qld_job_id="$(json "$WORKDIR/open-qld.json" '["id"]')"
publish_job "$qld_job_id" Draft Open

status="$(fleet_get "$elig_provider_token" '/v1/fleet/jobs?limit=100' "$WORKDIR/open-feed.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/open-feed.json"; fail "GET /v1/fleet/jobs returned $status, want 200"; }
python3 - "$WORKDIR/open-feed.json" "$open_job_id" "$qld_job_id" <<'PY' || fail "the feed is not Docs/10 §4.5's envelope, or it carries a job outside the provider's service area"
import json, sys
page = json.load(open(sys.argv[1]))
if set(page) - {"data", "next_cursor", "has_more"}:
    print("unexpected keys:", set(page), file=sys.stderr); sys.exit(1)
if not isinstance(page["data"], list) or "has_more" not in page:
    print("wrong shape:", page, file=sys.stderr); sys.exit(1)
ids = [job["id"] for job in page["data"]]
if sys.argv[2] not in ids:
    print("the eligible job is missing:", ids, file=sys.stderr); sys.exit(1)
if sys.argv[3] in ids:
    print("a Queensland job reached a provider who serves Victoria only", file=sys.stderr); sys.exit(1)
created = [job["created_at"] for job in page["data"]]
if created != sorted(created, reverse=True):
    print("not newest first:", created, file=sys.stderr); sys.exit(1)
PY
ok "the envelope is data/next_cursor/has_more, newest first, and a job outside the service area is not in it"

# A second eligible job, so paging has a boundary to cross. Same pickup, so the same declaration
# and the same truck accept it.
status="$(fleet_request POST "$fleet_customer_token" "verify-open-second-$$" /v1/jobs \
  '{"pickup":{"line":"7 Swan Street","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"3 Collins Street","suburb":"Melbourne","state":"VIC","postcode":"3000"},
    "goods_description":"Dining table","weight_kg":60,
    "budget_cents":120000}' \
  "$WORKDIR/open-second.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/open-second.json"; fail "creating the second eligible job returned $status"; }
second_job_id="$(json "$WORKDIR/open-second.json" '["id"]')"
publish_job "$second_job_id" Draft Open

# Paging followed the way a client follows it: take next_cursor, send it back, stop at
# has_more=false. The page size is 1 so the boundary is crossed once per job.
python3 - "$elig_provider_token" "$VERIFY_PORT" "$auth_header" "$open_job_id" "$second_job_id" <<'PY' || fail "paging the feed did not reach every eligible job exactly once"
import json, sys, urllib.parse, urllib.request

token, port, header = sys.argv[1], sys.argv[2], sys.argv[3]
must_appear = set(sys.argv[4:])
base = f"http://localhost:{port}/v1/fleet/jobs"

def get(url):
    request = urllib.request.Request(url, headers={header: f"Bearer {token}"})
    with urllib.request.urlopen(request) as response:
        return json.load(response)

whole = get(f"{base}?limit=100")
if whole["has_more"]:
    print("more than 100 eligible jobs; this check assumes there are not", file=sys.stderr)
    sys.exit(1)
expected = [job["id"] for job in whole["data"]]

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

# A repeat and a skip are both invisible to a set comparison, so the length is checked too.
if len(seen) != len(expected) or sorted(seen) != sorted(expected):
    print("paged over", seen, "want", expected, file=sys.stderr)
    sys.exit(1)
if not must_appear <= set(seen):
    print("the jobs this section published are not all in the feed:", must_appear - set(seen), file=sys.stderr)
    sys.exit(1)
print(f"    {len(expected)} eligible jobs over {pages} pages of one")
PY
ok "paging one job at a time reaches every eligible job exactly once, and terminates"

status="$(fleet_get "$elig_provider_token" '/v1/fleet/jobs?cursor=not-a-cursor' "$WORKDIR/open-badcursor.json")"
[[ "$status" == "400" ]] || fail "a mangled cursor returned $status, want 400"
status="$(fleet_get "$elig_provider_token" '/v1/fleet/jobs?limit=0' "$WORKDIR/open-badlimit.json")"
[[ "$status" == "400" ]] || fail "?limit=0 returned $status, want 400"
status="$(fleet_get "$elig_provider_token" '/v1/fleet/jobs?limit=5000' "$WORKDIR/open-biglimit.json")"
[[ "$status" == "200" ]] || fail "?limit=5000 returned $status, want it narrowed to the maximum"
ok "a mangled cursor and a bad limit are refused; an over-large limit is narrowed"

# A customer reaching the provider's feed gets an empty page rather than a 403. Being eligible for
# nothing is the truthful answer to the question, and the client renders that from an empty list.
status="$(fleet_get "$fleet_customer_token" /v1/fleet/jobs "$WORKDIR/open-customer.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/open-customer.json"; fail "a customer reading the feed returned $status, want 200"; }
[[ "$(tr -d ' \n' < "$WORKDIR/open-customer.json")" == '{"data":[],"has_more":false}' ]] \
  || { cat "$WORKDIR/open-customer.json"; fail "a customer's feed is not an empty array"; }
ok "a caller eligible for nothing gets an empty array, never null and never a 403"

# ---------------------------------------------------------------------------------------
ticket "SHIP-83  GET /v1/fleet/jobs/{id} — one job, no budget, and no doorstep"

status="$(fleet_get "$elig_provider_token" "/v1/fleet/jobs/$open_job_id" "$WORKDIR/open-detail.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/open-detail.json"; fail "GET /v1/fleet/jobs/$open_job_id returned $status, want 200"; }

# One shape, whatever the client did to obtain it — the rule the vehicle endpoints already follow,
# and here it is also the privacy decision: two shapes would be two places a budget field could be
# added and two responses this section would have to know to check.
python3 - "$WORKDIR/open-detail.json" "$WORKDIR/open-feed.json" "$open_job_id" <<'PY' || fail "the detail view and the feed entry are different shapes"
import json, sys
detail = json.load(open(sys.argv[1]))
entry = next(job for job in json.load(open(sys.argv[2]))["data"] if job["id"] == sys.argv[3])
if detail != entry:
    print("detail:", detail, "\nfeed:  ", entry, file=sys.stderr)
    sys.exit(1)
PY
ok "the job by identifier is the entry the feed carried, field for field"

# **The invariant, checked where it can actually be broken: the bytes a provider receives.**
#
# Docs/11 §8 records what the SHIP-67 pairing still owed — "its provider response, serialised,
# asserted to carry no budget". internal/fleet/openjobs_test.go is that test in Go, over a closed
# set of keys so that a budget renamed `max_price` fails too. This is the same assertion made from
# outside Go, against a running service, so that neither can be quietly deleted alone.
stored_budget="$("$PSQL" "$DATABASE_URL" -tAc \
  "select budget from jobs where id = '$open_job_id';")"
[[ -n "$stored_budget" ]] || fail "the job carries no budget, so these checks are asserting nothing"
[[ "$stored_budget" == "1500.00" ]] || fail "the stored budget is '$stored_budget', want 1500.00"

python3 - "$WORKDIR/open-detail.json" "$WORKDIR/open-feed.json" <<'PY' || fail "the customer's budget reached a provider"
import json, re, sys

# Every key a provider may see, at any depth: the envelope's three, the job's fifteen, and the two
# nested shapes'. **A closed list, and that is the point** — a deny-list of names catches
# `budget_cents` and misses `max_price`, while Docs/01 §4.3 forbids the budget "as an amount, a
# band, or a 'budget supplied' flag" rather than forbidding a spelling.
allowed = {
    "data", "next_cursor", "has_more",
    "id", "status", "pickup", "dropoff", "goods_description",
    "length_cm", "width_cm", "height_cm", "weight_kg",
    "vehicle_requirement", "handling_notes", "pickup_window", "dropoff_window",
    "expires_at", "created_at",
    "suburb", "state", "postcode", "start", "end",
}

def keys(node):
    if isinstance(node, dict):
        for key, child in node.items():
            yield key
            yield from keys(child)
    elif isinstance(node, list):
        for child in node:
            yield from keys(child)

# Identifiers come out before the value search: a UUID is hexadecimal, so a run of digits can
# occur inside one by chance — rarely enough to pass review and often enough to fail one morning.
identifier = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")

for path in sys.argv[1:]:
    raw = open(path).read()

    unexpected = sorted(set(keys(json.loads(raw))) - allowed)
    if unexpected:
        print(path, "carries keys this API never promised a provider:", unexpected, file=sys.stderr)
        print("A budget under another name is still a budget (Docs/01 §4.3).", file=sys.stderr)
        sys.exit(1)

    if "budget" in raw.lower():
        print(path, "mentions the budget:", raw, file=sys.stderr)
        sys.exit(1)

    searchable = identifier.sub("<id>", raw)
    for rendering in ("1500.00", "150000"):
        if rendering in searchable:
            print(path, "carries the budget's value as", rendering, file=sys.stderr)
            print(raw, file=sys.stderr)
            sys.exit(1)
PY
ok "the customer's budget is on the job and in neither response — not the word, not the value, not a key under another name"

# The other disclosure decision, confirmed by SHIP-83 rather than inherited: a provider who has not
# bid gets the locality and not the doorstep. The coordinate goes with the street line, because the
# platform geocodes the whole address — sending it would be sending the line as two numbers.
"$PSQL" "$DATABASE_URL" -q -c \
  "update jobs set pickup_latitude = -37.8197, pickup_longitude = 144.9989 where id = '$open_job_id';"
status="$(fleet_get "$elig_provider_token" "/v1/fleet/jobs/$open_job_id" "$WORKDIR/open-detail2.json")"
[[ "$status" == "200" ]] || fail "re-reading the job returned $status"
for disclosure in 'Church Street' 'Bourke Street' '37.8197' '144.9989' '"line"' '"coordinate"' '"latitude"'; do
  grep -q "$disclosure" "$WORKDIR/open-detail2.json" \
    && { cat "$WORKDIR/open-detail2.json"; fail "the provider's job carries '$disclosure' before they have bid"; }
done
grep -q 'Richmond' "$WORKDIR/open-detail2.json" || fail "the pickup locality is missing; a provider cannot price without it"
grep -q '3121' "$WORKDIR/open-detail2.json" || fail "the pickup postcode is missing"
ok "the pickup is suburb, state and postcode — never the street line, and never the coordinate that is the street line"

# A job this provider may not bid on is indistinguishable from one that does not exist. 403 would
# confirm the job is there, and which jobs a competitor may bid on is nobody else's business.
status="$(fleet_get "$elig_provider_token" "/v1/fleet/jobs/$qld_job_id" "$WORKDIR/open-theirs.json")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/open-theirs.json"; fail "an ineligible job returned $status, want 404"; }
status="$(fleet_get "$elig_provider_token" /v1/fleet/jobs/00000000-0000-7000-8000-000000000020 "$WORKDIR/open-nothing.json")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/open-nothing.json"; fail "a job that does not exist returned $status, want 404"; }
python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$WORKDIR/open-theirs.json" "$WORKDIR/open-nothing.json" \
  || fail "a job outside the provider's eligibility answers differently from a job that does not exist"
ok "an ineligible job answers exactly what a missing job answers — the refusal confirms nothing"

# And the customer cannot read their own job here. GET /v1/jobs/{id} is where they read it, and
# that response is the one shape in this API that carries the budget.
status="$(fleet_get "$fleet_customer_token" "/v1/fleet/jobs/$open_job_id" "$WORKDIR/open-owner.json")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/open-owner.json"; fail "the owning customer read their job through the provider's route: $status"; }
ok "the provider's route is not a second way to a job the customer owns"

# ---------------------------------------------------------------------------------------
ticket "SHIP-83a  the feed moved off the {id} slot, and the old path is gone rather than aliased"

# **What only a running service can show.** cmd/api/routes_jobsegment_test.go proves the freed space
# by attaching a four-segment literal to the real route table; what it cannot prove is that the
# process this harness has been driving for the last two hundred checks came up at all. It did — the
# collision was a *registration* panic, so every check above ran against a binary that would not have
# started if the pair had been reintroduced.
#
# What is left to show here is the other half of the Done when: the old path is gone, not aliased.
# A redirect would collide with a four-segment literal exactly as a 200 does, because ServeMux
# refuses the pair before either handler is reached — so an alias is not a gentler migration, it is
# the same defect wearing a 301.

# **`/v1/jobs/open` answers 400 and that is the demonstration rather than a near miss.** The word
# `open` now lands in `GET /v1/jobs/{id}`'s identifier slot and is refused for not being a UUID —
# which is precisely the claim SHIP-83a makes: the slot holds an identifier again, and no literal is
# shadowing it. A 404 here would mean something was still matching the old shape.
status="$(fleet_get "$elig_provider_token" /v1/jobs/open "$WORKDIR/moved-feedpath.json")"
[[ "$status" == "400" ]] \
  || { cat "$WORKDIR/moved-feedpath.json"; fail "GET /v1/jobs/open returned $status, want 400 — the word open should now be read as a job identifier and refused for not being one"; }
python3 - "$WORKDIR/moved-feedpath.json" <<'FEEDPATH' || fail "the refusal is not the malformed-identifier one"
import json, sys
if json.load(open(sys.argv[1]))["error"]["code"] != "bad_request":
    sys.exit("the old feed path is being served by something: %s" % open(sys.argv[1]).read())
FEEDPATH

# And the four-segment form matches nothing at all: no route, no redirect, no alias. A redirect
# would collide with a four-segment literal exactly as a 200 does, because ServeMux refuses the pair
# before either handler is reached — an alias is the same defect wearing a 301, not a gentler
# migration. curl is not following redirects here, so one would show as its own status.
status="$(fleet_get "$elig_provider_token" "/v1/jobs/open/$open_job_id" "$WORKDIR/moved-detailpath.json")"
[[ "$status" == "404" ]] \
  || { cat "$WORKDIR/moved-detailpath.json"; fail "GET /v1/jobs/open/{id} returned $status, want 404 — SHIP-83a removed the old path and did not alias it"; }
ok "the old feed path is now read as a job identifier and the old detail path matches nothing — gone rather than aliased"

# And the new ones answer, which is what makes the 404s above a move rather than a deletion.
status="$(fleet_get "$elig_provider_token" '/v1/fleet/jobs?limit=100' "$WORKDIR/moved-feed.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/moved-feed.json"; fail "GET /v1/fleet/jobs returned $status, want 200"; }
status="$(fleet_get "$elig_provider_token" "/v1/fleet/jobs/$open_job_id" "$WORKDIR/moved-detail.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/moved-detail.json"; fail "GET /v1/fleet/jobs/{id} returned $status, want 200"; }
ok "the feed and the single job answer under /v1/fleet, which is where the provider's own things already live"

# ---------------------------------------------------------------------------------------
ticket "SHIP-96a  GET /v1/fleet/jobs/{id} once the job has left the feed — the bid, not eligibility"

# **What only the harness can show here.** internal/fleet's tests drive the handler on a mux of
# their own against a bid row they insert; this drives the served route, past the real auth class,
# against a job published through /v1/jobs by a real customer and moved through the guarded
# transition. The two halves that meet only here are the route and the predicate.
#
# The bid is written with psql rather than through POST /v1/jobs/{id}/bids, and that is deliberate
# rather than a shortcut: this section belongs to fleet, the bidding endpoints are 61-bidding.sh's
# to exercise, and what SHIP-96a asserts is that a *row in bids* opens the read. Placing the offer
# through the API would make this check depend on another section's fixtures for no extra evidence.

status="$(fleet_request POST "$fleet_customer_token" "verify-ship96a-job-$$" /v1/jobs \
  '{"pickup":{"line":"5 Church Street","suburb":"Richmond","state":"VIC","postcode":"3121"},
    "dropoff":{"line":"1 Bourke Street","suburb":"Melbourne","state":"VIC","postcode":"3000"},
    "goods_description":"Two-seater sofa","weight_kg":80,"length_cm":190,"width_cm":90,"height_cm":80,
    "budget_cents":432199}' \
  "$WORKDIR/awarded-job.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/awarded-job.json"; fail "creating the SHIP-96a job returned $status"; }
awarded_job_id="$(json "$WORKDIR/awarded-job.json" '["id"]')"
publish_job "$awarded_job_id" Draft Open

# The fixture, verified rather than assumed: a privacy check against a job with nothing to leak
# passes forever and proves nothing.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from jobs where id = '$awarded_job_id' and budget is not null;" | tr -d ' ')" == "1" ]] \
  || fail "the SHIP-96a job carries no budget, so the disclosure checks below assert nothing"

# While it is open the provider reads it because they are eligible — the SHIP-83 behaviour, restated
# here so the 200 after the award is a widening rather than a job that never left.
status="$(fleet_get "$elig_provider_token" "/v1/fleet/jobs/$awarded_job_id" "$WORKDIR/awarded-before.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/awarded-before.json"; fail "an eligible provider returned $status, want 200"; }

"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
  "insert into bids (id, job_id, provider_id, status, amount)
   values (gen_random_uuid(), '$awarded_job_id', '$elig_provider_id', 'Accepted', 45000);"
publish_job "$awarded_job_id" Open Awarded

# The feed no longer carries it. This is the half that makes the next check mean something: without
# it, a 200 below could be a job that is simply still biddable.
status="$(fleet_get "$elig_provider_token" '/v1/fleet/jobs?limit=100' "$WORKDIR/awarded-feed.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/awarded-feed.json"; fail "the feed returned $status"; }
python3 -c "
import json, sys
page = json.load(open(sys.argv[1]))
sys.exit(1 if any(j['id'] == sys.argv[2] for j in page['data']) else 0)
" "$WORKDIR/awarded-feed.json" "$awarded_job_id" \
  || fail "an awarded job is still in the open feed — SHIP-96a widens the single-job read and must not widen the feed"

status="$(fleet_get "$elig_provider_token" "/v1/fleet/jobs/$awarded_job_id" "$WORKDIR/awarded-after.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/awarded-after.json"; \
  fail "the awarded provider returned $status, want 200 — before SHIP-96a winning a job was how a provider lost sight of it"; }
[[ "$(json "$WORKDIR/awarded-after.json" '["status"]')" == "awarded" ]] \
  || { cat "$WORKDIR/awarded-after.json"; fail "the status is not awarded; it is the only field that says what became of the job"; }
ok "a provider holding a bid reads the job after it leaves the feed, and the status says what became of it"

# The same shape, not a fuller one. Two shapes would be two places a budget field could be added.
python3 -c "
import json, sys
before = set(json.load(open(sys.argv[1])))
after  = set(json.load(open(sys.argv[2])))
extra  = after - before
if extra:
    print('the awarded read carries keys the feed shape does not:', sorted(extra))
    sys.exit(1)
" "$WORKDIR/awarded-before.json" "$WORKDIR/awarded-after.json" \
  || fail "the widened read answers with a different shape"

# And no budget, in any form, on the path the widening opened. The word, the amount in dollars and
# in cents, and the sentence — the last is the form no key list and no value search can see, and it
# is the one wave 10 proved a whole suite can miss.
for disclosure in budget 4321.99 432199 maximum ceiling 'willing to pay' 'up to $'; do
  grep -qi -- "$disclosure" "$WORKDIR/awarded-after.json" \
    && { cat "$WORKDIR/awarded-after.json"; fail "the awarded provider's job carries '$disclosure'"; }
done
ok "the widened read carries no budget — not the word, not the value, and not a sentence about one"

# A provider with neither relationship gets exactly what a missing job gets — and the control is
# built rather than assumed. The second provider is given a bid on a **different** job first, so the
# 404 below cannot be read as "this account holds no bids at all". That is the parenthesisation
# check at the harness: `WHERE id = $3 AND eligible OR bid` without the brackets binds the identifier
# to the first branch alone, and every provider holding any bid would read every job on the platform.
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
  "insert into bids (id, job_id, provider_id, status, amount)
   values (gen_random_uuid(), '$elig_job_id', '$fleet_other_id', 'Submitted', 39000);"

status="$(fleet_get "$fleet_other_token" "/v1/fleet/jobs/$awarded_job_id" "$WORKDIR/awarded-stranger.json")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/awarded-stranger.json"; fail "a provider with no bid returned $status, want 404"; }
status="$(fleet_get "$fleet_other_token" /v1/fleet/jobs/00000000-0000-7000-8000-000000000021 "$WORKDIR/awarded-missing.json")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/awarded-missing.json"; fail "a job that does not exist returned $status, want 404"; }
python3 -c "
import json, sys
a = json.load(open(sys.argv[1]))['error']
b = json.load(open(sys.argv[2]))['error']
sys.exit(0 if (a['code'], a['message']) == (b['code'], b['message']) else 1)
" "$WORKDIR/awarded-stranger.json" "$WORKDIR/awarded-missing.json" \
  || fail "a job the caller has no bid on answers differently from a job that does not exist"
ok "a provider with neither eligibility nor a bid gets exactly what a missing job gets"
