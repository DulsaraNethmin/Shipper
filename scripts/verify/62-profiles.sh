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

# ---------------------------------------------------------------------------------------
ticket "SHIP-81b  a provider photographs each of Docs/04 §3's four documents and uploads them directly"

# The *Done when* has four clauses and only one of them can be demonstrated here rather than in a Go
# test: **"uploads it directly through a short-lived pre-signed URL, with the API never in the path
# of the bytes"**. So this section signs, uploads to a host that is not the API, reads the object
# back out of the store itself, and then proves the same object is refused without a signature.
#
# The other three — the kind, the verification record it belongs to, and the record's own coherence —
# are held by `internal/profiles`'s tests against a real database, and by
# `ck_provider_verification_documents_kind` underneath them.
#
# # Everything here fences on ids
#
# The object key carries the provider id and a fresh UUIDv7, and the bucket may be shared with four
# other worktrees (CLAUDE.md's worktree table). So no check counts objects or lists a prefix; each
# one names the key it created, which is 70-delivery.sh's rule and the same rule CLAUDE.md states for
# Kafka.

prof_post() {
  curl -s -X POST -o "$4" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$3" \
    "http://localhost:$VERIFY_PORT/v1/provider/verification/documents$5"
}

# --- the URL, and what it is signed for --------------------------------------------------------

status="$(prof_post "$prof_provider_token" "verify-prof-doc-$$" \
  '{"content_type":"image/jpeg","content_length":52}' "$WORKDIR/prof-doc-url.json" "/uploads")"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/prof-doc-url.json"; fail "minting a document upload URL answered $status, want 200"; }

doc_key="$(json "$WORKDIR/prof-doc-url.json" '["object_key"]')"
doc_url="$(json "$WORKDIR/prof-doc-url.json" '["upload_url"]')"
doc_method="$(json "$WORKDIR/prof-doc-url.json" '["method"]')"
doc_type="$(json "$WORKDIR/prof-doc-url.json" '["content_type"]')"
doc_expires="$(json "$WORKDIR/prof-doc-url.json" '["expires_at"]')"

[[ "$doc_method" == "PUT" && "$doc_type" == "image/jpeg" ]] \
  || fail "the response describes a $doc_method of $doc_type"
[[ "$doc_key" == verification/$prof_provider_id/* ]] \
  || fail "object_key is $doc_key, want it prefixed by verification/$prof_provider_id/"
ok "the platform chose the key, prefixed with the provider it was issued to"

# 200 rather than 201, because nothing was created. The request that creates something is the
# submission below, and that one is a 201.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
      "select count(*) from provider_verification_documents where object_key = '$doc_key';")" == "0" ]] \
  || fail "issuing an upload URL wrote a document row"
ok "and issuing it wrote nothing — an object in a bucket is evidence of nothing until it is submitted"

# The URL is the object store's and is not a route on this service. If SHIP-81b were ever
# "simplified" into a proxy upload, this is the check that fails first.
[[ "$doc_url" == "$STORAGE_ENDPOINT/"* ]] \
  || fail "upload_url is $doc_url, which is not the object store at $STORAGE_ENDPOINT"
[[ "$doc_url" != *"localhost:$VERIFY_PORT"* && "$doc_url" != *"/v1/"* ]] \
  || fail "upload_url points back at this service: $doc_url"
ok "the URL is the object store's, not this API's — the platform is not in the path of the bytes"

doc_window="$(python3 - "$doc_url" "$doc_expires" <<'PY'
import sys, urllib.parse, datetime
query = urllib.parse.parse_qs(urllib.parse.urlsplit(sys.argv[1]).query)
signed = int(query["X-Amz-Expires"][0])
expires = datetime.datetime.fromisoformat(sys.argv[2].replace("Z", "+00:00"))
ahead = (expires - datetime.datetime.now(datetime.timezone.utc)).total_seconds()
print(signed, int(ahead))
PY
)"
read -r doc_signed_seconds doc_seconds_ahead <<<"$doc_window"
(( doc_signed_seconds > 0 && doc_signed_seconds <= 3600 )) \
  || fail "the URL is signed to last $doc_signed_seconds seconds; internal/config caps the lifetime at an hour because nothing can revoke one"
(( doc_seconds_ahead > 0 && doc_seconds_ahead <= doc_signed_seconds + 5 )) \
  || fail "expires_at is $doc_seconds_ahead seconds away against a signed window of $doc_signed_seconds"
ok "the URL is short-lived — $doc_signed_seconds seconds in the signature, and expires_at agrees with it"

# --- the upload itself, with this service in neither direction ---------------------------------

printf '%s' 'not a licence, but exactly fifty-two bytes of proof.' > "$WORKDIR/prof-doc.bin"
[[ "$(wc -c < "$WORKDIR/prof-doc.bin" | tr -d ' ')" == "52" ]] || fail "the fixture is not 52 bytes"

put_status="$(curl -s -o /dev/null -w '%{http_code}' -X PUT \
  -H "Content-Type: $doc_type" --data-binary "@$WORKDIR/prof-doc.bin" "$doc_url")"
[[ "$put_status" == "200" ]] \
  || fail "the pre-signed PUT answered $put_status — the provider could not upload directly, which is the whole of this clause"
ok "the provider uploaded the document straight to the object store with that URL and nothing else"

stored_doc="$("${COMPOSE[@]}" exec -T minio sh -c \
  'mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null; \
   mc cat "local/'"$STORAGE_BUCKET/$doc_key"'"' 2>/dev/null)"
[[ "$stored_doc" == "not a licence, but exactly fifty-two bytes of proof." ]] \
  || fail "the object in $STORAGE_BUCKET reads back as '$stored_doc'"
ok "and the bytes are in $STORAGE_BUCKET under the key the API named, read back out of the store itself"

# --- the record: which kind, and whose verification record --------------------------------------

status="$(prof_post "$prof_provider_token" "verify-prof-doc-record-$$" \
  "{\"kind\":\"licence\",\"object_key\":\"$doc_key\"}" "$WORKDIR/prof-doc-record.json" "")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/prof-doc-record.json"; fail "submitting the licence answered $status, want 201"; }

doc_id="$(json "$WORKDIR/prof-doc-record.json" '["id"]')"
[[ "$(json "$WORKDIR/prof-doc-record.json" '["kind"]')" == "licence" ]] \
  || fail "the submission came back as something other than a licence"

# What was stored is what the *store* reported, not what the client declared: the request asked for
# 52 bytes and the object is 52 bytes, so this agrees — and the column is filled from the HEAD rather
# than from the body, which is what a row with no object behind it could never have.
recorded="$("$PSQL" "$DATABASE_URL" -tAc \
  "select kind || ' ' || provider_id || ' ' || content_type || ' ' || content_length || ' ' ||
          (length(etag) > 0)::text
     from provider_verification_documents where id = '$doc_id';")"
[[ "$recorded" == "licence $prof_provider_id image/jpeg 52 true" ]] \
  || fail "the recorded document is '$recorded', want 'licence $prof_provider_id image/jpeg 52 true'"
ok "the row names its kind, the verification record it belongs to, and what the store reported"

# --- the object is private, and reachable only by a fresh signed URL ---------------------------

unsigned_status="$(curl -s -o /dev/null -w '%{http_code}' "$STORAGE_ENDPOINT/$STORAGE_BUCKET/$doc_key")"
[[ "$unsigned_status" == "403" ]] \
  || fail "an unsigned GET of the document answered $unsigned_status, want 403 — a licence is the most identifying object this platform holds"
ok "the same object is refused without a signature: there is no public read path to a verification document"

# **The two reads are a second apart, deliberately, and that is a property of SigV4 rather than
# padding.** A pre-signed signature is a deterministic function of the key, the window and the
# signing instant — which has one-second resolution — so two reads inside the same second produce
# byte-identical URLs *even though each one was signed afresh*. String inequality is therefore the
# wrong instrument for "nothing stores a URL"; what says it is that the signing instant moves with
# the request, plus the schema having no column a URL could have come out of. Both are checked below.
status="$(prof_get "$prof_provider_token" /v1/provider/verification/documents "$WORKDIR/prof-docs-1.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/prof-docs-1.json"; fail "reading the documents answered $status"; }
sleep 1.1
status="$(prof_get "$prof_provider_token" /v1/provider/verification/documents "$WORKDIR/prof-docs-2.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/prof-docs-2.json"; fail "the second read answered $status"; }

doc_link_1="$(python3 -c "
import json,sys
for d in json.load(open(sys.argv[1]))['data']:
    if d['id'] == sys.argv[2]: print(d['download_url']); break
" "$WORKDIR/prof-docs-1.json" "$doc_id")"
doc_link_2="$(python3 -c "
import json,sys
for d in json.load(open(sys.argv[1]))['data']:
    if d['id'] == sys.argv[2]: print(d['download_url']); break
" "$WORKDIR/prof-docs-2.json" "$doc_id")"

[[ -n "$doc_link_1" && -n "$doc_link_2" ]] || fail "a submitted document came back with no download URL"

doc_signed_at_1="$(python3 -c "
import sys, urllib.parse
print(urllib.parse.parse_qs(urllib.parse.urlsplit(sys.argv[1]).query)['X-Amz-Date'][0])" "$doc_link_1")"
doc_signed_at_2="$(python3 -c "
import sys, urllib.parse
print(urllib.parse.parse_qs(urllib.parse.urlsplit(sys.argv[1]).query)['X-Amz-Date'][0])" "$doc_link_2")"
[[ "$doc_signed_at_1" != "$doc_signed_at_2" ]] \
  || fail "both reads carry the signing instant $doc_signed_at_1 — a stored URL would look exactly like this"
[[ "$doc_link_1" != "$doc_link_2" ]] \
  || fail "two reads a second apart produced the same URL, so the credential did not move with the request"

# And there is no column a URL could have come out of, which is what makes the freshness above a
# property of the schema rather than of one implementation.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
      "select count(*) from information_schema.columns
        where table_name = 'provider_verification_documents'
          and (column_name like '%url%' or column_name like '%link%');")" == "0" ]] \
  || fail "provider_verification_documents holds a URL-shaped column, so a credential is stored at rest"

signed_status="$(curl -s -o "$WORKDIR/prof-doc-fetched.bin" -w '%{http_code}' "$doc_link_1")"
[[ "$signed_status" == "200" ]] || fail "the signed download answered $signed_status"
[[ "$(cat "$WORKDIR/prof-doc-fetched.bin")" == "not a licence, but exactly fifty-two bytes of proof." ]] \
  || fail "the signed download returned something other than the uploaded bytes"
ok "each read mints a fresh signed URL, and it fetches the document the provider uploaded"

# The key never crosses the wire on a read. It is a durable handle into the bucket holding identity
# documents, and every read is answered with a short-lived credential instead.
grep -q "$doc_key" "$WORKDIR/prof-doc-record.json" \
  && { cat "$WORKDIR/prof-doc-record.json"; fail "the submission response carries the object key"; }
grep -q '"download_url"' "$WORKDIR/prof-doc-record.json" \
  && { cat "$WORKDIR/prof-doc-record.json"; fail "the 201 carries a download URL, which the idempotency middleware would then store and replay"; }
ok "no object key on the wire and no credential on the 201 — the replayed body carries nothing at rest"

# --- the four kinds, each captured independently ------------------------------------------------

# prof_submit_kind <kind> — mint, upload and submit one document, leaving its key in
# $prof_last_key.
#
# **It sets a global rather than printing the key, and it is called directly rather than in a
# command substitution.** `fail` ends the run with `exit 1`, and inside `$( … )` that exits the
# subshell — the harness would print the failure and carry on green. A helper that can fail has to
# run in the caller's shell.
#
# **Every call draws a fresh idempotency key from $prof_submissions.** A retake is a *second
# submission*, not a retry of the first, so re-using the key would have the middleware replay the
# original response and write no row — which is the middleware working, and would have made the
# append-only check below silently vacuous. It did, once, before the counter was added.
prof_submissions=0
prof_last_key=""
prof_submit_kind() {
  local kind="$1" url
  prof_submissions=$((prof_submissions + 1))

  status="$(prof_post "$prof_provider_token" "verify-prof-doc-$kind-$prof_submissions-$$" \
    '{"content_type":"image/jpeg","content_length":52}' \
    "$WORKDIR/prof-doc-$kind-$prof_submissions-url.json" "/uploads")"
  [[ "$status" == "200" ]] \
    || { cat "$WORKDIR/prof-doc-$kind-$prof_submissions-url.json"; fail "minting a URL for $kind answered $status"; }

  prof_last_key="$(json "$WORKDIR/prof-doc-$kind-$prof_submissions-url.json" '["object_key"]')"
  url="$(json "$WORKDIR/prof-doc-$kind-$prof_submissions-url.json" '["upload_url"]')"

  put_status="$(curl -s -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: image/jpeg" \
    --data-binary "@$WORKDIR/prof-doc.bin" "$url")"
  [[ "$put_status" == "200" ]] || fail "uploading the $kind answered $put_status"

  status="$(prof_post "$prof_provider_token" "verify-prof-doc-rec-$kind-$prof_submissions-$$" \
    "{\"kind\":\"$kind\",\"object_key\":\"$prof_last_key\"}" \
    "$WORKDIR/prof-doc-$kind-$prof_submissions.json" "")"
  [[ "$status" == "201" ]] \
    || { cat "$WORKDIR/prof-doc-$kind-$prof_submissions.json"; fail "submitting the $kind answered $status"; }
}

for kind in registration insurance abn_evidence; do
  prof_submit_kind "$kind"
done

submitted_kinds="$("$PSQL" "$DATABASE_URL" -tAc \
  "select string_agg(distinct kind, ',' order by kind)
     from provider_verification_documents where provider_id = '$prof_provider_id';")"
[[ "$submitted_kinds" == "abn_evidence,insurance,licence,registration" ]] \
  || fail "the provider's record holds '$submitted_kinds', want Docs/04 §3's four"
ok "all four of Docs/04 §3's documents are captured independently — licence, registration, insurance, ABN evidence"

# A retake is a new row rather than an edit: the replaced image is what an administrator already
# looked at, and Docs/04 §4's Restricted covers "document renewal" explicitly.
before_retake="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from provider_verification_documents where provider_id = '$prof_provider_id';")"
prof_submit_kind insurance
after_retake="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from provider_verification_documents where provider_id = '$prof_provider_id';")"
(( after_retake == before_retake + 1 )) \
  || fail "a retake moved the record from $before_retake documents to $after_retake — an image somebody reviewed was replaced"
ok "and a retake appends rather than overwrites: the evidence trail keeps what was reviewed"

# --- separation: one provider's key never lands on another's verification record ----------------

other_before="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from provider_verification_documents where provider_id = '$prof_other_id';")"

status="$(prof_post "$prof_other_token" "verify-prof-doc-steal-$$" \
  "{\"kind\":\"licence\",\"object_key\":\"$doc_key\"}" "$WORKDIR/prof-doc-steal.json" "")"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/prof-doc-steal.json"; fail "another provider submitted this provider's object: $status"; }
[[ "$(json "$WORKDIR/prof-doc-steal.json" '["error"]["details"][0]["field"]')" == "object_key" ]] \
  || { cat "$WORKDIR/prof-doc-steal.json"; fail "the refusal does not name object_key"; }

other_after="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from provider_verification_documents where provider_id = '$prof_other_id';")"
[[ "$other_after" == "$other_before" ]] \
  || fail "the second provider's record moved from $other_before documents to $other_after"

# The cross-table pair, over every row rather than the one just attempted: a document sits on the
# verification record its object key was minted for, or the platform has mixed two people's evidence.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
      "select count(*) from provider_verification_documents
        where object_key not like 'verification/' || provider_id::text || '/%';")" == "0" ]] \
  || fail "some documents are recorded against a verification record their object key does not name"
ok "a key issued to one provider cannot become another provider's evidence, and no stored row does"

# --- the platform asks the store before it writes anything -------------------------------------

status="$(prof_post "$prof_provider_token" "verify-prof-doc-ghost-$$" \
  '{"content_type":"image/jpeg","content_length":52}' "$WORKDIR/prof-doc-ghost-url.json" "/uploads")"
[[ "$status" == "200" ]] || fail "minting a URL for the unspent-key check answered $status"
ghost_key="$(json "$WORKDIR/prof-doc-ghost-url.json" '["object_key"]')"

status="$(prof_post "$prof_provider_token" "verify-prof-doc-ghost-rec-$$" \
  "{\"kind\":\"licence\",\"object_key\":\"$ghost_key\"}" "$WORKDIR/prof-doc-ghost.json" "")"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/prof-doc-ghost.json"; fail "submitting an object nobody uploaded answered $status, want 409"; }
[[ "$(json "$WORKDIR/prof-doc-ghost.json" '["error"]["code"]')" == "profiles_document_not_uploaded" ]] \
  || { cat "$WORKDIR/prof-doc-ghost.json"; fail "expected code=profiles_document_not_uploaded"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
      "select count(*) from provider_verification_documents where object_key = '$ghost_key';")" == "0" ]] \
  || fail "a row was written for an object the store does not hold"
ok "a submission the store cannot confirm is refused — the platform never records evidence it has not looked at"

# One object is evidence for at most one document, which is what stops a single photograph of a
# licence standing in for an insurance certificate as well.
status="$(prof_post "$prof_provider_token" "verify-prof-doc-twice-$$" \
  "{\"kind\":\"insurance\",\"object_key\":\"$doc_key\"}" "$WORKDIR/prof-doc-twice.json" "")"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/prof-doc-twice.json"; fail "one object became two documents: $status"; }
[[ "$(json "$WORKDIR/prof-doc-twice.json" '["error"]["code"]')" == "profiles_document_already_recorded" ]] \
  || { cat "$WORKDIR/prof-doc-twice.json"; fail "expected code=profiles_document_already_recorded"; }
ok "and one uploaded image can be at most one document"

# --- who may reach any of this ------------------------------------------------------------------

status="$(prof_post "$prof_customer_token" "verify-prof-doc-cust-$$" \
  '{"content_type":"image/jpeg","content_length":52}' "$WORKDIR/prof-doc-cust.json" "/uploads")"
[[ "$status" == "403" ]] || { cat "$WORKDIR/prof-doc-cust.json"; fail "a customer was issued an upload URL: $status"; }
[[ "$(json "$WORKDIR/prof-doc-cust.json" '["error"]["code"]')" == "profiles_provider_only" ]] \
  || { cat "$WORKDIR/prof-doc-cust.json"; fail "expected code=profiles_provider_only"; }

status="$(curl -s -o "$WORKDIR/prof-doc-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/provider/verification/documents")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/prof-doc-anon.json"; fail "an unauthenticated read returned $status, want 401"; }

status="$(curl -s -X POST -o "$WORKDIR/prof-doc-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $prof_provider_token" -H 'Content-Type: application/json' \
  -d '{"content_type":"image/jpeg","content_length":52}' \
  "http://localhost:$VERIFY_PORT/v1/provider/verification/documents/uploads")"
[[ "$status" == "400" ]] \
  || { cat "$WORKDIR/prof-doc-nokey.json"; fail "a request with no Idempotency-Key answered $status, want 400"; }
ok "a customer, an anonymous caller and a request with no Idempotency-Key are each refused"

# --- the trail is append-only, and there is no expiry column ------------------------------------

if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
     "update provider_verification_documents set kind = 'insurance' where id = '$doc_id';" \
     >/dev/null 2>&1; then
  fail "a submitted document was rewritten"
fi
if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
     "delete from provider_verification_documents where id = '$doc_id';" >/dev/null 2>&1; then
  fail "a submitted document was deleted"
fi
ok "the evidence trail is append-only — an image cannot be swapped after a decision was taken on it"

# The four, and only the four. Docs/04 §3 is the authority; a fifth would be a document no reviewer,
# no client and no queue knows about.
doc_accepted="$("$PSQL" "$DATABASE_URL" -tAc \
  "select pg_get_constraintdef(oid) from pg_constraint
    where conname = 'ck_provider_verification_documents_kind';")"
for kind in licence registration insurance abn_evidence; do
  grep -q "'$kind'" <<<"$doc_accepted" || fail "the CHECK does not accept Docs/04 §3's document $kind"
done
[[ "$(grep -o "'" <<<"$doc_accepted" | wc -l | tr -d ' ')" == "8" ]] \
  || fail "ck_provider_verification_documents_kind accepts something other than exactly four kinds: $doc_accepted"
ok "the database accepts Docs/04 §3's four documents and no fifth"

# **No expiry column, deliberately.** Docs/04 §3 gives the renewal cadence to legal and insurance
# advisers (Track-X row X-4) and says it "remains genuinely outside engineering's competence to
# settle". SHIP-159 is the ticket that adds it, with that answer in front of it.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
      "select count(*) from information_schema.columns
        where table_name = 'provider_verification_documents'
          and (column_name like '%expir%' or column_name like '%renew%');")" == "0" ]] \
  || fail "provider_verification_documents carries an expiry column, which X-4 has not answered"
ok "and no expiry column was invented — the renewal cadence is X-4's, and SHIP-159 is the ticket"
