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
  "{\"email\":\"fleet-provider-$$@example.com\",\"phone\":\"04140$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/fleet-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/fleet-provider.json"; fail "could not register the fleet provider: $status"; }
fleet_provider_id="$(json "$WORKDIR/fleet-provider.json" '["id"]')"
fleet_provider_token="$(mint_token "$fleet_provider_id")"

status="$(post_json "verify-fleet-other-$$" /v1/auth/register \
  "{\"email\":\"fleet-other-$$@example.com\",\"phone\":\"04141$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/fleet-other.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/fleet-other.json"; fail "could not register the second provider: $status"; }
fleet_other_token="$(mint_token "$(json "$WORKDIR/fleet-other.json" '["id"]')")"

status="$(post_json "verify-fleet-cust-$$" /v1/auth/register \
  "{\"email\":\"fleet-customer-$$@example.com\",\"phone\":\"04142$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
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
  "{\"email\":\"fleet-fresh-$$@example.com\",\"phone\":\"04143$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
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
ticket "SHIP-81  the eligibility filter — four filters, each shown to exclude something"

# SHIP-81 adds no endpoint. SHIP-82 puts `GET /v1/jobs/open` in front of the query and this
# section becomes HTTP checks then; until it does, the demonstration is the predicate run against
# real rows, from outside Go.
#
# # The SQL below is a MIRROR of internal/fleet/eligibility.go's `eligible`, and that is a cost
#
# internal/fleet/eligibility_test.go is authoritative: it exercises the real predicate through the
# real domain service, and it is what `make check` runs. What this adds is a different kind of
# evidence — that the four filters are answerable from the real schema, against rows created
# through the real API, with nothing Go-shaped in the path. A copy that drifts would be a check
# quietly asserting the wrong thing, so it is written once, next to the section it serves, and
# SHIP-82 deletes it in favour of the endpoint.
#
# A provider of its own, deliberately. The sections above leave several vehicles in various states
# on $fleet_provider_id, and "no vehicle in service" cannot be demonstrated against a fleet whose
# contents this check does not control.

status="$(post_json "verify-elig-prov-$$" /v1/auth/register \
  "{\"email\":\"fleet-eligible-$$@example.com\",\"phone\":\"04144$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
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

# The job carries a budget, deliberately. It is what the last check in this section reads: the
# feed's column list must not name it, and a job with no budget at all would make that assertion
# vacuous.
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

# eligible — 1 when the provider may bid on that one job, 0 when any filter excludes it.
#
# Scoped to the single job by id, so the jobs 50-jobs.sh leaves behind cannot make a broken filter
# look like a working one.
eligible() {
  "$PSQL" "$DATABASE_URL" -tAc "
    select count(*) from jobs j
     where j.id = '$elig_job_id'
       and j.status in ('Open', 'Negotiating')
       and (j.expires_at is null or j.expires_at > now())
       and j.customer_id <> '$elig_provider_id'
       and exists (select 1 from users u
                    where u.id = '$elig_provider_id' and u.role = 'provider'
                      and u.status = 'active'
                      and u.email_verified_at is not null
                      and u.phone_verified_at is not null)
       and exists (select 1 from provider_service_areas a
                    where a.provider_id = '$elig_provider_id'
                      and ((a.scope = 'state'    and a.area = j.pickup_state)
                        or (a.scope = 'postcode' and a.area = j.pickup_postcode)))
       and exists (select 1 from vehicles v
                    where v.provider_id = '$elig_provider_id' and v.deactivated_at is null
                      and (j.weight_kg is null or v.max_weight_kg  is null or v.max_weight_kg  >= j.weight_kg)
                      and (j.length_cm is null or v.load_length_cm is null or v.load_length_cm >= j.length_cm)
                      and (j.width_cm  is null or v.load_width_cm  is null or v.load_width_cm  >= j.width_cm)
                      and (j.height_cm is null or v.load_height_cm is null or v.load_height_cm >= j.height_cm));"
}

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

# The invariant this feed exists under, checked where it can actually be broken.
#
# The provider's feed is the first thing in the service that reads `jobs` from outside the jobs
# domain, and it reads it column by column — so the SELECT list *is* the disclosure boundary, and
# `budget` sits three lines from `weight_kg` in the same table. SHIP-67's source-parsing guard
# parses internal/jobs and cannot see this; internal/fleet has its own copy, and this is the same
# assertion made from outside Go so that neither can be quietly deleted alone.
stored_budget="$("$PSQL" "$DATABASE_URL" -tAc \
  "select budget is not null from jobs where id = '$elig_job_id';")"
[[ "$stored_budget" == "t" ]] || fail "the job carries no budget, so this check is asserting nothing"

selected="$(sed -n '/^const eligibleJobColumns/,/`$/p' services/core/internal/fleet/eligibility.go)"
[[ -n "$selected" ]] || fail "eligibleJobColumns is gone; the provider feed's column list is what this reads"
if grep -qi 'budget' <<<"$selected"; then
  fail "the provider feed's column list names the budget, which Docs/01 §4.3 forbids in every form"
fi
ok "the customer's budget is stored on the job and named nowhere in the provider feed's column list"
