# shellcheck shell=bash
#
# M3 profiles — a provider's verification standing, and the guard that makes it unsettable.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 62–69 is free in the fleet-and-bidding range: 60 is fleet and 61 is bidding, and this is the first
# file `internal/profiles` has owned. A track adds a file and edits none (Docs/11 §9, SHIP-15e).
#
# # Mobile numbers, and this file's block
#
# Sections are *sourced* into one process, and every account registered anywhere needs a unique
# mobile — so two sections drawing the same prefix collide on the phone unique index. The allocation
# is recorded in 61-bidding.sh's header: 04120 identity, 0413x jobs, 0414x fleet, 0416x bidding,
# 0417x delivery, 04180 outbox, 0419x admin. **This file takes 0415x**, which is between fleet's
# block and bidding's and is used by nothing else on the tree at SHIP-81a.
#
# # What the eligibility half of SHIP-81a is demonstrated by, and it is not here
#
# "Only Verified bids" is a claim about `internal/fleet`'s predicate, so it is demonstrated in
# 60-fleet.sh against `GET /v1/fleet/jobs` — the endpoint a provider's phone actually calls. Putting
# it here as well would be a second description of the rule, which is the thing SHIP-81a exists to
# avoid. What this file covers is the record: its states, its guard, its trail, and the read a
# provider makes of their own.
#
# # Every token here claims `role: customer`, including the ones that succeed
#
# mint_token puts `role: customer` in every token it issues, which is exactly the thing worth
# exercising: the platform decides what a caller may do by reading `users.role`, not by believing the
# claim. So the provider below is answered with a token that says customer, and the customer is
# refused with an identical one.
#
# No check here signs in, so SHIP-47's per-address bucket is untouched by this file.

# --- the accounts these checks run as ----------------------------------------------------------

status="$(post_json "verify-prof-prov-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"profiles-provider-$$@example.com\",\"phone\":\"04150$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/prof-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/prof-provider.json"; fail "could not register the profiles provider: $status"; }
prof_provider_id="$(json "$WORKDIR/prof-provider.json" '["id"]')"
prof_provider_token="$(mint_token "$prof_provider_id")"

status="$(post_json "verify-prof-other-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"profiles-other-$$@example.com\",\"phone\":\"04151$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/prof-other.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/prof-other.json"; fail "could not register the second provider: $status"; }
prof_other_id="$(json "$WORKDIR/prof-other.json" '["id"]')"
prof_other_token="$(mint_token "$prof_other_id")"

status="$(post_json "verify-prof-cust-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"profiles-customer-$$@example.com\",\"phone\":\"04152$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/prof-customer.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/prof-customer.json"; fail "could not register the profiles customer: $status"; }
prof_customer_id="$(json "$WORKDIR/prof-customer.json" '["id"]')"
prof_customer_token="$(mint_token "$prof_customer_id")"

# prof_get <token> <path> <outfile> — one authenticated read. No Idempotency-Key: a GET changes
# nothing, and a check that sent one would be demonstrating something other than what it claims.
prof_get() {
  curl -s -o "$3" -w '%{http_code}' -H "$auth_header: Bearer $1" \
    "http://localhost:$VERIFY_PORT$2"
}

# prof_decide <user-id> <state> <reason> — the one guarded transition, called directly.
#
# **There is one HTTP route to a decision and it is not on this credential.**
# `POST /v1/admin/verifications/{id}/decision` (SHIP-154) is an administrator's act on the
# administrator credential, and it is demonstrated end to end in 90-admin.sh — including that a
# provider holding their own token cannot reach it. This file has no administrator session and is
# not the place to acquire one, so it drives the platform's own function, which is what that
# endpoint calls underneath: `provider_verification_decide` is the single implementation of a
# transition, and the domain, the console and this fixture are the same caller.
prof_decide() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
    "select provider_verification_decide('$1', '$2', 'system', null, '$3');" >/dev/null
}

prof_state() {
  "$PSQL" "$DATABASE_URL" -tAc "select state from provider_verifications where provider_id = '$1';"
}

# ---------------------------------------------------------------------------------------
ticket "SHIP-81a  every provider has a verification record from the moment they register"

# The decision this ticket had to take about who creates the record, demonstrated end to end.
# SHIP-153's queue is "pending provider verifications listed oldest first", and a queue over a table
# holding only *decided* providers lists nobody who is waiting — so Pending has to be a row, and the
# row has to arrive without anybody asking for it.
[[ "$(prof_state "$prof_provider_id")" == "Pending" ]] \
  || fail "a provider who has just registered is not Pending: $(prof_state "$prof_provider_id")"
ok "registering as a provider creates the record, at Pending — nothing had to ask for it"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
      "select count(*) from provider_verifications where provider_id = '$prof_customer_id';")" == "0" ]] \
  || fail "a customer has a verification record"
ok "and a customer has none — most accounts are customers, and a queue of them reviews nobody"

# The backfill, which is the other half of "every provider". Providers registered before 000200
# exist on this database from earlier runs and from earlier waves; none of them may be without a
# record, or the predicate cannot tell "not verified" from "never asked".
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
      "select count(*) from users u
        where u.role = 'provider'
          and not exists (select 1 from provider_verifications v where v.provider_id = u.id);")" == "0" ]] \
  || fail "some provider accounts have no verification record"
ok "and every provider on the database has one, including the ones that predate the migration"

# ---------------------------------------------------------------------------------------
ticket "SHIP-81a  GET /v1/provider/verification — a provider reads their own state and no other's"

status="$(curl -s -o "$WORKDIR/prof-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/provider/verification")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/prof-anon.json"; fail "an unauthenticated read returned $status, want 401"; }
ok "it cannot be reached without a credential"

status="$(prof_get "$prof_provider_token" /v1/provider/verification "$WORKDIR/prof-pending.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/prof-pending.json"; fail "GET /v1/provider/verification returned $status, want 200"; }
[[ "$(json "$WORKDIR/prof-pending.json" '["state"]')" == "Pending" ]] \
  || { cat "$WORKDIR/prof-pending.json"; fail "a new provider does not read Pending"; }
python3 -c "
import json, sys
body = json.load(open(sys.argv[1]))
missing = [f for f in ('reason', 'decided_at') if f in body]
if missing:
    print('an undecided record carries', missing)
    sys.exit(1)
if 'submitted_at' not in body:
    print('the record carries no submitted_at, so a Pending screen cannot say how long')
    sys.exit(1)
" "$WORKDIR/prof-pending.json" || fail "the undecided shape is wrong"
ok "a provider nobody has reviewed reads Pending, with no reason and no decision clock"

status="$(prof_get "$prof_customer_token" /v1/provider/verification "$WORKDIR/prof-cust.json")"
[[ "$status" == "403" ]] || { cat "$WORKDIR/prof-cust.json"; fail "a customer read the endpoint: $status"; }
[[ "$(json "$WORKDIR/prof-cust.json" '["error"]["code"]')" == "profiles_provider_only" ]] \
  || { cat "$WORKDIR/prof-cust.json"; fail "expected code=profiles_provider_only"; }
ok "a customer is refused with a code the app can act on — and both tokens claim 'customer'"

# Two providers, two different states, each reading their own. There is no parameter for whose
# record to read, so this is the strongest demonstration available that the query is scoped: it
# fails in either direction if it ever stopped being.
prof_decide "$prof_provider_id" Verified "Licence, registration, insurance and ABN all current."
prof_decide "$prof_other_id" Rejected "The licence is in a different name from the account."

status="$(prof_get "$prof_provider_token" /v1/provider/verification "$WORKDIR/prof-mine.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/prof-mine.json"; fail "the verified provider's read returned $status"; }
status="$(prof_get "$prof_other_token" /v1/provider/verification "$WORKDIR/prof-theirs.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/prof-theirs.json"; fail "the rejected provider's read returned $status"; }

[[ "$(json "$WORKDIR/prof-mine.json" '["state"]')" == "Verified" ]] || fail "the verified provider does not read Verified"
[[ "$(json "$WORKDIR/prof-theirs.json" '["state"]')" == "Rejected" ]] || fail "the rejected provider does not read Rejected"
[[ "$(json "$WORKDIR/prof-mine.json" '["reason"]')" != "$(json "$WORKDIR/prof-theirs.json" '["reason"]')" ]] \
  || fail "both providers read the same reason — the record is not scoped to the caller"
ok "two providers in different states each read their own record, reason included"

# The reason reaches the provider, because Docs/04 §4 requires a rejection's be communicable. The
# administrator does not, because the identity of whoever decided is what turns a moderation
# decision into a personal one.
grep -q "different name from the account" "$WORKDIR/prof-theirs.json" \
  || { cat "$WORKDIR/prof-theirs.json"; fail "the rejected provider is not told why"; }
for disclosure in admin_id decided_by reviewer administrator; do
  grep -qi -- "$disclosure" "$WORKDIR/prof-theirs.json" \
    && { cat "$WORKDIR/prof-theirs.json"; fail "the response carries '$disclosure'"; }
done
ok "the reason reaches the provider and the deciding administrator does not"

# ---------------------------------------------------------------------------------------
ticket "SHIP-81a  the state is not a settable field, and every transition records actor and reason"

if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
     "update provider_verifications set state = 'Verified' where provider_id = '$prof_other_id';" \
     >/dev/null 2>&1; then
  fail "a direct UPDATE set a verification state"
fi
[[ "$(prof_state "$prof_other_id")" == "Rejected" ]] || fail "the refused UPDATE changed the state anyway"
ok "a bare UPDATE is refused — which is what a support query at a psql prompt looks like"

if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
     "insert into provider_verifications (provider_id, state)
      values ('$prof_customer_id', 'Verified');" >/dev/null 2>&1; then
  fail "a verification record was created at Verified"
fi
ok "and a record cannot be created in any state but Pending — every other one is reached by decision"

# Naming a real decision that describes a different move. This is the case a guard that merely
# checked "some decision exists" would let through, and it is the reason the trigger compares both
# ends rather than counting rows.
if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 <<SQL >/dev/null 2>&1
begin;
select set_config('shipper.provider_verification_decision',
                  (select id::text from provider_verification_decisions
                    where provider_id = '$prof_other_id' limit 1), true);
update provider_verifications set state = 'Verified' where provider_id = '$prof_other_id';
commit;
SQL
then
  fail "a decision recording one move authorised a different one"
fi
[[ "$(prof_state "$prof_other_id")" == "Rejected" ]] || fail "the state moved on a decision that does not describe it"
ok "and a real decision authorises only the move it describes, not any move at all"

# The trail Docs/04 §6.6 requires: the decision, the actor, the timestamp and the reason.
trail="$("$PSQL" "$DATABASE_URL" -tAc \
  "select from_state || ' ' || to_state || ' ' || actor_type || ' ' ||
          coalesce(actor_id::text, 'none') || ' ' || (decided_at is not null)::text
     from provider_verification_decisions where provider_id = '$prof_other_id';")"
[[ "$trail" == "Pending Rejected system none true" ]] \
  || fail "the recorded decision is '$trail', want 'Pending Rejected system none true'"
ok "the decision records both ends, its actor, its clock and its reason"

if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
     "update provider_verification_decisions set reason = 'changed my mind'
       where provider_id = '$prof_other_id';" >/dev/null 2>&1; then
  fail "a recorded decision was rewritten"
fi
if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
     "delete from provider_verification_decisions where provider_id = '$prof_other_id';" \
     >/dev/null 2>&1; then
  fail "a recorded decision was deleted"
fi
ok "and the trail is append-only — a rejection cannot be rewritten as an approval afterwards"

# A decision that changes nothing is refused. Docs/04 §6.6 requires a reason with every outcome, and
# a second decision with no change to show for it is a review that did not happen.
if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
     "select provider_verification_decide('$prof_other_id', 'Rejected', 'system', null, 'again');" \
     >/dev/null 2>&1; then
  fail "a decision from a state to itself was recorded"
fi
ok "a decision that changes nothing is refused rather than recorded"

# The five, and only the five. Docs/04 §4 is the authority and ck_provider_verifications_state is
# where the platform holds it; a sixth would be a state no client, no queue and no predicate knows.
accepted="$("$PSQL" "$DATABASE_URL" -tAc \
  "select pg_get_constraintdef(oid) from pg_constraint
    where conname = 'ck_provider_verifications_state';")"
for state in Pending Verified Restricted Rejected Suspended; do
  grep -q "'$state'" <<<"$accepted" || fail "the CHECK does not accept Docs/04 §4's outcome $state"
done
[[ "$(grep -o "'" <<<"$accepted" | wc -l | tr -d ' ')" == "10" ]] \
  || fail "ck_provider_verifications_state accepts something other than exactly five states: $accepted"
ok "the database accepts Docs/04 §4's five outcomes and no sixth"
