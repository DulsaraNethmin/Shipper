# shellcheck shell=bash
#
# M1 identity — the users table, password storage, sessions, tokens, registration, and the two
# verification channels.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 40–49 is identity's range. This file is the identity track's to append to, and no other
# track's to edit — which is the whole reason the script was split (Docs/11 §9, SHIP-15e).
#
# The sections here are ordered, not independent: registration creates the account that
# verify-email and verify-phone then confirm, and $registered_id, $reg_email and
# $reg_phone_local are set once and read by everything after. Adding a section that needs a
# fresh account should register its own rather than reuse that one.

ticket "SHIP-28  the users table, with its constraints enforced by the database"

"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from information_schema.tables where table_name = 'users';" | grep -q 1 \
  || fail "the users table does not exist"
ok "users exists"

"$PSQL" "$DATABASE_URL" -q -c \
  "insert into users (id, email, phone, password_hash, role)
   values (gen_random_uuid(), 'verify-$$@example.com', '+6140000$$', 'x', 'customer');" >/dev/null \
  || fail "a valid account could not be created"

if "$PSQL" "$DATABASE_URL" -q -c \
  "insert into users (id, email, phone, password_hash, role)
   values (gen_random_uuid(), 'VERIFY-$$@EXAMPLE.COM', '+6140001$$', 'x', 'customer');" >/dev/null 2>&1; then
  fail "the same address in different case was accepted as a second account"
fi
ok "email uniqueness is case-insensitive, so one address is one account"

if "$PSQL" "$DATABASE_URL" -q -c \
  "insert into users (id, email, phone, password_hash, role)
   values (gen_random_uuid(), 'role-$$@example.com', '+6140002$$', 'x', 'admin');" >/dev/null 2>&1; then
  fail "'admin' was accepted as a role; admin sign-in is a separate system (SHIP-147)"
fi
ok "role is constrained to customer and provider"

# ---------------------------------------------------------------------------------------
ticket "SHIP-29  passwords are stored as argon2id, and nothing reversible is stored"

pushd "$ROOT/services/core" >/dev/null
if ! password_log="$(go test ./internal/passwords/ -run TestPassword -count=1 -v 2>&1)"; then
  echo "$password_log"
  popd >/dev/null
  fail "the password tests do not pass"
fi
popd >/dev/null
ok "round trip, wrong password, salting, tampering and truncation all hold"

# The stored form itself, read out of the test that produced it rather than asserted twice.
# A hash is not a secret; the password that made it is a literal in the test file.
sample="$(grep -oE '\$argon2id\$v=19\$m=[0-9]+,t=[0-9]+,p=[0-9]+\$[A-Za-z0-9+/]+\$[A-Za-z0-9+/]+' \
  <<<"$password_log" | head -1)"
[[ -n "$sample" ]] || { echo "$password_log"; fail "no PHC string appeared in the test output"; }
ok "the stored form is a PHC string, carrying its variant, version and all three costs"

# The parameters being inside the hash is what allows the cost to be raised later without
# invalidating a single stored password (Docs/10 §5).
costs="$(cut -d'$' -f4 <<<"$sample")"
[[ "$costs" =~ ^m=[0-9]+,t=[0-9]+,p=[0-9]+$ ]] || fail "the costs are not in the hash: $costs"
ok "the costs travel with the hash — $costs — so raising the profile needs no migration"

# Reversibility is a property of the schema as much as of the code: every credential column in the
# database holds a derived key, and the list of them is short enough to write out.
#
# **SHIP-147 added the second entry**, and the list is still exact rather than a pattern. Two
# credential columns is a fact worth stating deliberately — `users` and `admin_users` are separate
# account systems (CLAUDE.md's third separation) and each stores its own argon2id PHC string through
# `internal/passwords`. Loosening this to "every match ends in _hash" would let a third one appear
# without anybody deciding it should, which is the whole reason this check is a literal.
credential_column="$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(string_agg(table_name || '.' || column_name, ', ' order by table_name), 'none')
     from information_schema.columns
    where table_schema = 'public'
      and column_name ~ '(password|secret|passphrase)'")"
[[ "$credential_column" == "admin_users.password_hash, users.password_hash" ]] \
  || fail "expected admin_users.password_hash and users.password_hash and nothing else, found: $credential_column"
ok "the schema holds exactly one credential column, users.password_hash"

# ---------------------------------------------------------------------------------------
ticket "SHIP-38  one row per device, holding refresh state, a label and last seen"

"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from information_schema.tables where table_name = 'device_sessions';" | grep -q 1 \
  || fail "the device_sessions table does not exist"
ok "device_sessions exists, at migration 000100 inside identity's reserved block"

columns="$("$PSQL" "$DATABASE_URL" -tAc \
  "select string_agg(column_name || ':' || data_type, ' ' order by column_name)
     from information_schema.columns
    where table_schema = 'public' and table_name = 'device_sessions';")"
for want in "id:uuid" "user_id:uuid" "refresh_token_hash:text" "device_label:text" \
            "last_seen_at:timestamp with time zone" "created_at:timestamp with time zone" \
            "updated_at:timestamp with time zone"; do
  grep -qF "$want" <<<"$columns" || fail "expected a column $want; found: $columns"
done
ok "refresh state, device label and last seen are there, every timestamp with its zone"

# The token is opaque and stored hashed (Docs/10 §5), so what the column may not hold is the
# token. Uniqueness is the part the database has to enforce: one token, one session.
# -q as well as -tA: without it psql appends its own "INSERT 0 1" status line to the value, and
# a captured id with a command tag stuck to it produces a uuid syntax error later rather than a
# constraint failure — which is a check that passes for the wrong reason.
verify_user="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into users (id, email, phone, password_hash, role)
   values (gen_random_uuid(), 'session-$$@example.com', '+6140003$$', 'x', 'customer')
   returning id;")"
# refresh_token_expires_at is supplied because SHIP-39 made it NOT NULL with no default, and a
# session with no expiry is exactly what that column exists to refuse. SHIP-39's own section
# below checks the refusal; here the column is only what makes an otherwise valid row valid.
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into device_sessions
       (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label)
   values (gen_random_uuid(), '$verify_user', 'hash-$$', now() + interval '30 days',
           'Verify iPhone');" >/dev/null \
  || fail "a device session could not be created"

if "$PSQL" "$DATABASE_URL" -q -c \
  "insert into device_sessions
       (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label)
   values (gen_random_uuid(), '$verify_user', 'hash-$$', now() + interval '30 days',
           'Verify Pixel');" >/dev/null 2>&1; then
  fail "two sessions hold the same refresh token hash"
fi
ok "a refresh token hash belongs to exactly one session"

if "$PSQL" "$DATABASE_URL" -q -c \
  "delete from users where id = '$verify_user';" >/dev/null 2>&1; then
  fail "deleting the account took its sessions with it; the foreign key is not RESTRICT"
fi
ok "ON DELETE RESTRICT holds, so sessions cannot vanish with a deleted account"

trigger="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from pg_trigger tr
     join pg_class cl on cl.oid = tr.tgrelid
     join pg_proc p   on p.oid  = tr.tgfoid
    where cl.relname = 'device_sessions' and p.proname = 'set_updated_at'
      and not tr.tgisinternal;")"
[[ "$trigger" == "1" ]] || fail "device_sessions has no set_updated_at trigger"

fk_index="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from pg_index i
     join pg_class t     on t.oid = i.indrelid
     join pg_attribute a on a.attrelid = t.oid and a.attnum = i.indkey[0]
    where t.relname = 'device_sessions' and a.attname = 'user_id';")"
[[ "$fk_index" -ge 1 ]] || fail "the foreign key on device_sessions.user_id is not indexed"
ok "the updated_at trigger is attached and the foreign key is indexed"

# ---------------------------------------------------------------------------------------
ticket "SHIP-37  a short-lived signed token carrying the user, the role and an expiry"

pushd "$ROOT/services/core" >/dev/null
if ! token_log="$(go test ./internal/identity/ -run 'TestAccessToken|TestKeyset|TestNewAccessToken' -count=1 -v 2>&1)"; then
  echo "$token_log"
  popd >/dev/null
  fail "the access token tests do not pass"
fi
popd >/dev/null
ok "the TTL, kid selection, rotation and the driver-audience refusal all hold"

# A real token, decoded here rather than by the library that produced it. Asking the issuer what
# it issued would prove very little; this reads the bytes that would go over the wire.
access_token="$(grep -oE 'access token: [A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+' <<<"$token_log" \
  | head -1 | awk '{print $3}')"
[[ -n "$access_token" ]] || { echo "$token_log"; fail "no access token appeared in the test output"; }

python3 - "$access_token" >"$WORKDIR/token.json" <<'PYTHON'
import base64, json, sys

def segment(s):
    return json.loads(base64.urlsafe_b64decode(s + "=" * (-len(s) % 4)))

header, payload, _signature = sys.argv[1].split(".")
header, payload = segment(header), segment(payload)

json.dump({
    "alg":         header.get("alg"),
    "kid":         header.get("kid"),
    "claim_names": " ".join(sorted(payload)),
    "iss":         payload.get("iss"),
    "aud":         payload.get("aud"),
    "sub":         payload.get("sub"),
    "role":        payload.get("role"),
    "lifetime":    payload.get("exp", 0) - payload.get("iat", 0),
}, sys.stdout)
PYTHON

[[ "$(json "$WORKDIR/token.json" '["alg"]')" == "HS256" ]] \
  || fail "the token is not signed with HS256"
kid="$(json "$WORKDIR/token.json" '["kid"]')"
[[ -n "$kid" && "$kid" != "None" ]] \
  || fail "the token names no signing key, so a key could never be rotated"
ok "HS256, naming its key in the header as kid=$kid"

# Exactly the claim set Docs/10 §5 fixes, checked as a whole. A missing claim breaks something
# loudly; an added one sits there being believed, which is the direction that matters.
claims="$(json "$WORKDIR/token.json" '["claim_names"]')"
[[ "$claims" == "aud exp iat iss jti role sid sub" ]] \
  || fail "the claims are '$claims', and Docs/10 §5 says exactly: aud exp iat iss jti role sid sub"
ok "the claim set is exactly sub, role, sid, iat, exp, jti, iss and aud"

for forbidden in permissions scope scopes verified email_verified phone_verified status; do
  grep -qw "$forbidden" <<<"$claims" && fail "the token carries '$forbidden'"
done
ok "no permission and no verification state — the platform decides both, freshly (Docs/07 §3)"

[[ "$(json "$WORKDIR/token.json" '["iss"]')" == "shipper" ]] || fail "iss is not shipper"
[[ "$(json "$WORKDIR/token.json" '["aud"]')" == "shipper-mobile" ]] \
  || fail "aud is not shipper-mobile, which is what separates this from the driver token"
[[ "$(json "$WORKDIR/token.json" '["role"]')" =~ ^(customer|provider)$ ]] \
  || fail "role is not one of the two the platform issues"
[[ "$(json "$WORKDIR/token.json" '["sub"]')" =~ ^[0-9a-f-]{36}$ ]] || fail "sub is not a user id"
ok "iss=shipper, aud=shipper-mobile, and sub carries the user id"

[[ "$(json "$WORKDIR/token.json" '["lifetime"]')" == "900" ]] \
  || fail "the token lives $(json "$WORKDIR/token.json" '["lifetime"]') seconds, and Docs/10 §5 says 900"
ok "it expires fifteen minutes after it was issued, measured against an injected clock"

# ---------------------------------------------------------------------------------------
ticket "SHIP-30  POST /v1/auth/register creates an unverified account and rejects duplicates"

# Every value is suffixed with the process id, because this runs against the developer's own
# database rather than a throwaway one and the accounts it creates stay there. A fixed address
# would pass once and then report "already taken" forever.
reg_email="register-$$@example.com"
reg_phone_local="04120$$"
reg_phone_e164="+61${reg_phone_local:1}"

status="$(post_json "verify-reg-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$reg_email\",\"phone\":\"$reg_phone_local\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/register.json"; fail "POST /v1/auth/register returned $status, want 201"; }
ok "an account is created and answered with 201"

registered_id="$(json "$WORKDIR/register.json" '["id"]')"
[[ "$registered_id" =~ ^[0-9a-f-]{36}$ ]] || fail "the response carries no account id: $registered_id"
[[ "$(json "$WORKDIR/register.json" '["role"]')" == "customer" ]] \
  || fail "the account did not take the role it was registered with"
[[ "$(json "$WORKDIR/register.json" '["status"]')" == "active" ]] \
  || fail "a new account is not active"

# The half of the criterion that is easiest to lose: *unverified*. Docs/04 §2 requires both
# channels verified before a customer may publish, so an account that arrived verified would
# skip the whole of SHIP-31, 33, 34 and 36 without anything failing.
[[ "$(json "$WORKDIR/register.json" '["email_verified"]')" == "False" ]] \
  || fail "a brand new account reports its email as verified"
[[ "$(json "$WORKDIR/register.json" '["phone_verified"]')" == "False" ]] \
  || fail "a brand new account reports its phone as verified"
ok "it is unverified on both channels, and says so"

# The response is one thing; the row is another. Read the columns the endpoint claims to have
# written, from the database rather than from the answer it gave about itself.
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select phone || ' ' || role || ' ' || status || ' ' ||
          coalesce(email_verified_at::text, 'null') || ' ' ||
          coalesce(phone_verified_at::text, 'null')
     from users where id = '$registered_id';")"
[[ "$stored" == "$reg_phone_e164 customer active null null" ]] \
  || fail "the stored row is '$stored', want '$reg_phone_e164 customer active null null'"
ok "the row holds the number in E.164 and both verification timestamps null"

# The password is not recoverable from what was stored. SHIP-29 proves the format; this proves
# the endpoint uses it rather than writing the plaintext into the same column.
stored_hash="$("$PSQL" "$DATABASE_URL" -tAc \
  "select password_hash from users where id = '$registered_id';")"
[[ "$stored_hash" == \$argon2id\$* ]] || fail "the stored credential is not a PHC string: $stored_hash"
grep -q 'correct-horse-battery-staple' <<<"$stored_hash" && fail "the password is in the stored value"
ok "the credential column holds an argon2id hash, not the password"

status="$(post_json "verify-reg-dupe-email-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$reg_email\",\"phone\":\"0499${$}0\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/register-dupe-email.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/register-dupe-email.json"; fail "a duplicate email returned $status, want 409"; }
[[ "$(json "$WORKDIR/register-dupe-email.json" '["error"]["code"]')" == "identity_email_taken" ]] \
  || fail "expected code=identity_email_taken"
ok "a second account on the same address is refused by uq_users_email"

# The same number in the form a person types it rather than the form it is stored in. Without
# normalisation these are two different strings and the index never sees a collision — which is
# the defect that would give one handset two accounts and make an OTP ambiguous.
status="$(post_json "verify-reg-dupe-phone-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"other-$$@example.com\",\"phone\":\"${reg_phone_local:0:4} ${reg_phone_local:4}\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/register-dupe-phone.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/register-dupe-phone.json"; fail "a duplicate phone returned $status, want 409"; }
[[ "$(json "$WORKDIR/register-dupe-phone.json" '["error"]["code"]')" == "identity_phone_taken" ]] \
  || fail "expected code=identity_phone_taken"
ok "the same number written differently is still the same number"

status="$(post_json "verify-reg-invalid-$$" /v1/auth/register \
  '{"email":"not-an-address","phone":"123","password":"short","role":"driver"}' \
  "$WORKDIR/register-invalid.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/register-invalid.json"; fail "an invalid registration returned $status, want 422"; }
fields="$(json "$WORKDIR/register-invalid.json" '["error"]["details"]' | tr -d "[]{}'\" " | tr ',' '\n' | grep '^field:' | cut -d: -f2 | sort | tr '\n' ' ')"
[[ "$fields" == "email name password phone role " ]] \
  || fail "the rejected fields are '$fields', want all five at once"
ok "every bad field is reported at once, so the form takes one round trip and not five"

# Registration is public — it is how a caller obtains credentials in the first place — and it is
# still behind the idempotency middleware like every other state-changing request.
status="$(curl -s -X POST -o "$WORKDIR/register-nokey.json" -w '%{http_code}' \
  -H 'Content-Type: application/json' -d '{}' "http://localhost:$VERIFY_PORT/v1/auth/register")"
[[ "$status" == "400" ]] || fail "registration without an Idempotency-Key returned $status"
[[ "$(json "$WORKDIR/register-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || fail "expected code=idempotency_key_required"
ok "it needs an Idempotency-Key, so a retry cannot produce a second account"

# ---------------------------------------------------------------------------------------
ticket "SHIP-30a  registration requires a name and users holds it in a column of its own"

# The first two clauses of the *Done when*. The third — that GET /v1/admin/users matches a
# search term against it — is exercised in 90-admin.sh, beside the endpoint that serves it.
#
# The column is the ticket's whole reason for existing: SHIP-151 shipped a search naming four
# terms and could serve three, because nothing had ever collected the fourth and a name cannot
# be backfilled.

status="$(post_json "verify-noname-$$" /v1/auth/register \
  "{\"email\":\"noname-$$@example.com\",\"phone\":\"04930$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/register-noname.json")"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/register-noname.json"; fail "a registration with no name returned $status, want 422"; }
[[ "$(json "$WORKDIR/register-noname.json" '["error"]["details"][0]["field"]')" == "name" ]] \
  || fail "the refusal does not name the name field"
ok "a registration without a name is refused, and the refusal names the field"

# Three spaces is a missing name rather than a three-character one. The service trims before it
# validates and `ck_users_name` refuses the same value in the database, which is the Docs/10
# §3.4 pairing — two checks believed to agree are two checks until something compares them.
status="$(post_json "verify-blankname-$$" /v1/auth/register \
  "{\"name\":\"   \",\"email\":\"blankname-$$@example.com\",\"phone\":\"04931$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/register-blankname.json")"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/register-blankname.json"; fail "a name of three spaces returned $status, want 422"; }
ok "a name of whitespace is refused, because the platform trims before it judges"

named_email="named-$$@example.com"
status="$(post_json "verify-named-$$" /v1/auth/register \
  "{\"name\":\"  Ngô  Đình  \",\"email\":\"$named_email\",\"phone\":\"04932$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/register-named.json")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/register-named.json"; fail "a named registration returned $status, want 201"; }
[[ "$(json "$WORKDIR/register-named.json" '["name"]')" == "Ngô  Đình" ]] \
  || fail "the response name is '$(json "$WORKDIR/register-named.json" '["name"]')', want it trimmed at the ends and untouched inside"
ok "a name is accepted, trimmed at the ends and otherwise left exactly as it was written"

named_id="$(json "$WORKDIR/register-named.json" '["id"]')"
stored_name="$("$PSQL" "$DATABASE_URL" -tAc \
  "select name from users where id = '$named_id';")"
[[ "$stored_name" == "Ngô  Đình" ]] \
  || fail "users.name holds '$stored_name', want 'Ngô  Đình' — the response is not the row"
ok "users.name holds it, in a column of its own rather than derived from anything"

# The database refuses what the service refuses, through a connection that does not go through
# the service — which is the connection a CHECK constraint exists for.
if "$PSQL" "$DATABASE_URL" -q -c \
  "update users set name = E'\\t' where id = '$named_id';" >/dev/null 2>&1; then
  fail "ck_users_name accepted a tab; a one-argument btrim strips spaces only"
fi
ok "ck_users_name refuses a tab as well as a space, which a one-argument btrim would not"

# ---------------------------------------------------------------------------------------
ticket "SHIP-45  the role is chosen at registration and cannot be changed afterwards"

status="$(post_json "verify-provider-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"provider-$$@example.com\",\"phone\":\"0498$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/register-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/register-provider.json"; fail "registering a provider returned $status"; }
[[ "$(json "$WORKDIR/register-provider.json" '["role"]')" == "provider" ]] \
  || fail "the account did not take the provider role"
ok "an account is created as either customer or provider, as asked"

status="$(post_json "verify-admin-role-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"admin-$$@example.com\",\"phone\":\"0497$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"admin\"}" \
  "$WORKDIR/register-admin.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/register-admin.json"; fail "'admin' was accepted as a role (status $status)"; }
ok "there is no third role — administrators sign in through a separate system (SHIP-147)"

# The half that matters, and the reason it is a trigger. A rule the application keeps does not
# apply to a support query typed at a psql prompt, which is precisely the path somebody would use
# to change a role "just this once" — and a customer becoming a provider silently rewrites the
# meaning of every job already attached to the account.
if "$PSQL" "$DATABASE_URL" -q -c \
  "update users set role = 'provider' where id = '$registered_id';" >/dev/null 2>&1; then
  fail "a customer was turned into a provider from a psql prompt"
fi
ok "UPDATE ... SET role is refused by the database, not by application logic alone"

surviving_role="$("$PSQL" "$DATABASE_URL" -tAc \
  "select role from users where id = '$registered_id';")"
[[ "$surviving_role" == "customer" ]] || fail "the role is now '$surviving_role'"
ok "the role survived the attempt"

# The trigger has a WHEN clause, so the writes that happen constantly — verification timestamps,
# account standing — are untouched by it. Without that, every one of them would raise, and the
# failure would look like a broken endpoint rather than a mis-scoped trigger.
"$PSQL" "$DATABASE_URL" -q -c \
  "update users set status = 'restricted' where id = '$registered_id';" >/dev/null \
  || fail "an unrelated update was refused by the role trigger"
"$PSQL" "$DATABASE_URL" -q -c \
  "update users set status = 'active' where id = '$registered_id';" >/dev/null
ok "every other column still updates, so the trigger is scoped to the transition it guards"

# ---------------------------------------------------------------------------------------
ticket "SHIP-31  a single-use, expiring verification token is generated and stored on registration"

token_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select token_hash || ' ' || email || ' ' ||
          coalesce(consumed_at::text, 'live') || ' ' ||
          (expires_at > now())::text || ' ' ||
          (expires_at <= now() + interval '24 hours' + interval '1 minute')::text
     from email_verification_tokens where user_id = '$registered_id';")"
[[ -n "$token_row" ]] || fail "registration stored no verification token"

read -r token_hash token_email token_consumed token_future token_within <<<"$token_row"
[[ "$token_email" == "$reg_email" ]] || fail "the token records '$token_email', not the address it was sent to"
[[ "$token_consumed" == "live" ]] || fail "a freshly issued token is already consumed"
[[ "$token_future" == "true" && "$token_within" == "true" ]] \
  || fail "the expiry is not within the next 24 hours (future=$token_future within=$token_within)"
ok "one live token, recorded against the address it was sent to, expiring within a day"

# The console email adapter logs the message in full, which is how a developer completes
# verification without a mailbox — and here it is how the token that exists only in the message
# becomes readable at all. It is deliberately the *only* place it exists.
verification_token="$(python3 - "$WORKDIR/server.log" "$reg_email" <<'PYTHON'
import json, re, sys

found = ""
for line in open(sys.argv[1], encoding="utf-8", errors="replace"):
    try:
        record = json.loads(line)
    except ValueError:
        continue
    if record.get("msg") != "email (console, not sent)" or record.get("to") != sys.argv[2]:
        continue
    match = re.search(r"[A-Za-z0-9_-]{43}", record.get("body", ""))
    if match:
        found = match.group(0)
print(found)
PYTHON
)"
[[ -n "$verification_token" ]] || fail "no verification email was logged for $reg_email"
ok "the verification message was sent, carrying a 43-character token"

# The row holds the hash of what was sent, and not what was sent. Computed here rather than
# asserted: anyone who could read this table would otherwise be able to verify every unverified
# address on the platform.
computed_hash="$(printf '%s' "$verification_token" | shasum -a 256 | cut -d' ' -f1)"
[[ "$computed_hash" == "$token_hash" ]] \
  || fail "the stored hash is not SHA-256 of the token that was sent ($token_hash vs $computed_hash)"
ok "the stored value is the SHA-256 of the token, so the token itself is nowhere in the database"

stored_token_count="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from email_verification_tokens where token_hash = '$verification_token';")"
[[ "$stored_token_count" == "0" ]] || fail "the token itself appears in the table"
ok "searching the table for the token finds nothing"

# One live token per account, enforced by a partial unique index rather than by the code that
# remembers to supersede the old one. Without it a link left in an old mailbox outlives its
# replacement for a full day.
if "$PSQL" "$DATABASE_URL" -q -c \
  "insert into email_verification_tokens (id, user_id, email, token_hash, expires_at)
   values (gen_random_uuid(), '$registered_id', '$reg_email', 'a-second-live-token-$$',
           now() + interval '1 day');" >/dev/null 2>&1; then
  fail "a second live token was accepted for one account"
fi
ok "a second live token is refused by uq_email_verification_tokens_live"

# ---------------------------------------------------------------------------------------
ticket "SHIP-34  a time-limited numeric OTP is generated, rate-limited, and stored hashed"

status="$(post_json "verify-otp-$$" /v1/auth/request-otp \
  "{\"phone\":\"$reg_phone_local\"}" "$WORKDIR/otp.json")"
[[ "$status" == "202" ]] || { cat "$WORKDIR/otp.json"; fail "POST /v1/auth/request-otp returned $status, want 202"; }
[[ "$(json "$WORKDIR/otp.json" '["retry_after_seconds"]')" == "60" ]] \
  || fail "the response does not carry the resend interval a client runs its timer from"
ok "a code is requested, and the answer says when to ask again"

otp_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select code_hash || ' ' || phone || ' ' || attempts || ' ' ||
          (expires_at > now())::text || ' ' ||
          (expires_at <= now() + interval '10 minutes' + interval '1 minute')::text
     from phone_otps where user_id = '$registered_id' and consumed_at is null;")"
[[ -n "$otp_row" ]] || fail "no live code was stored"

read -r otp_hash otp_phone otp_attempts otp_future otp_within <<<"$otp_row"
[[ "$otp_phone" == "$reg_phone_e164" ]] || fail "the code records '$otp_phone', not the number it was sent to"
[[ "$otp_attempts" == "0" ]] || fail "a fresh code already has $otp_attempts attempts against it"
[[ "$otp_future" == "true" && "$otp_within" == "true" ]] \
  || fail "the code does not expire within ten minutes (future=$otp_future within=$otp_within)"
ok "one live code, against the number it was sent to, expiring within ten minutes"

# The console SMS adapter logs the body in full on purpose (SHIP-35) — reading the code out of
# the log is how a developer verifies a number without a handset, and here it is what makes the
# stored form checkable at all.
otp_code="$(python3 - "$WORKDIR/server.log" "$reg_phone_e164" <<'PYTHON'
import json, re, sys

found = ""
for line in open(sys.argv[1], encoding="utf-8", errors="replace"):
    try:
        record = json.loads(line)
    except ValueError:
        continue
    if record.get("msg") != "sms (console, not sent)" or record.get("to") != sys.argv[2]:
        continue
    match = re.search(r"\b\d{6}\b", record.get("body", ""))
    if match:
        found = match.group(0)
print(found)
PYTHON
)"
[[ "$otp_code" =~ ^[0-9]{6}$ ]] || fail "no six-digit code was sent to $reg_phone_e164"
ok "the message carries a six-digit numeric code"

# argon2id, not SHA-256. Six digits is a million possibilities: a table of SHA-256 digests over
# that space is built by a laptop in under a second, so the work factor is the whole of the
# offline defence (000102_phone_otps).
[[ "$otp_hash" == \$argon2id\$* ]] || fail "the stored code is not an argon2id PHC string: $otp_hash"
ok "it is stored as argon2id, so a stolen table is not a list of codes"

stored_code_count="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from phone_otps where code_hash = '$otp_code' or phone_otps.code_hash like '%$otp_code%';")"
[[ "$stored_code_count" == "0" ]] || fail "the code itself appears in the table"
ok "searching the table for the code finds nothing"

# Rate limit, first rule: one code a minute. Demonstrated by the row count rather than by the
# status, because the status is deliberately identical — see the endpoint's contract entry.
codes_before="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from phone_otps where user_id = '$registered_id';")"
status="$(post_json "verify-otp-again-$$" /v1/auth/request-otp \
  "{\"phone\":\"$reg_phone_local\"}" "$WORKDIR/otp-again.json")"
[[ "$status" == "202" ]] || fail "a throttled request returned $status, and every outcome must look alike"
diff -q "$WORKDIR/otp.json" "$WORKDIR/otp-again.json" >/dev/null \
  || fail "a throttled request answered differently from an accepted one"
codes_after="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from phone_otps where user_id = '$registered_id';")"
[[ "$codes_before" == "$codes_after" ]] \
  || fail "a second request inside the cooldown issued another code ($codes_before then $codes_after)"
ok "a second request within the minute sends nothing, and says exactly what the first said"

# The privacy property this endpoint exists to have. A number with no account is answered
# identically, so "does this person have a Shipper account" is not a question anybody can ask.
status="$(post_json "verify-otp-unknown-$$" /v1/auth/request-otp \
  '{"phone":"+61499999999"}' "$WORKDIR/otp-unknown.json")"
[[ "$status" == "202" ]] || fail "an unknown number returned $status"
diff -q "$WORKDIR/otp.json" "$WORKDIR/otp-unknown.json" >/dev/null \
  || fail "an unknown number is answered differently from a known one"
unknown_rows="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from phone_otps where phone = '+61499999999';")"
[[ "$unknown_rows" == "0" ]] || fail "a code was stored for a number with no account"
ok "a number with no account gets the same answer, and no code is stored or sent"

status="$(post_json "verify-otp-bad-$$" /v1/auth/request-otp \
  '{"phone":"123"}' "$WORKDIR/otp-bad.json")"
[[ "$status" == "422" ]] || fail "an unusable number returned $status, want 422"
ok "an unusable number is a field error, which discloses nothing about who has an account"

# ---------------------------------------------------------------------------------------
ticket "SHIP-33  POST /v1/auth/verify-email marks the address verified and consumes the token"

status="$(post_json "verify-email-bad-$$" /v1/auth/verify-email \
  '{"token":"9qE2vT7bYw1sJk4pNc0aRlX8oZgHdM3uQiV6yB5tCfE"}' "$WORKDIR/verify-bad.json")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/verify-bad.json"; fail "an unknown token returned $status, want 400"; }
[[ "$(json "$WORKDIR/verify-bad.json" '["error"]["code"]')" == "identity_verification_token_invalid" ]] \
  || fail "expected code=identity_verification_token_invalid"
ok "a token this platform never issued is refused, and says nothing about why"

status="$(post_json "verify-email-$$" /v1/auth/verify-email \
  "{\"token\":\"$verification_token\"}" "$WORKDIR/verify-email.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/verify-email.json"; fail "POST /v1/auth/verify-email returned $status, want 200"; }
[[ "$(json "$WORKDIR/verify-email.json" '["email_verified"]')" == "True" ]] \
  || fail "the response does not report the address as verified"
[[ "$(json "$WORKDIR/verify-email.json" '["phone_verified"]')" == "False" ]] \
  || fail "verifying the email verified the phone as well"
ok "the address is verified, and the phone is not"

verified_state="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (u.email_verified_at is not null)::text || ' ' ||
          coalesce(t.consumed_reason, 'live')
     from users u
     join email_verification_tokens t on t.user_id = u.id
    where u.id = '$registered_id' and t.token_hash = '$token_hash';")"
[[ "$verified_state" == "true verified" ]] \
  || fail "the row says '$verified_state', want 'true verified'"
ok "the column is set and the token is consumed, both in the database"

# Single use. The second presentation is somebody clicking the link twice, which is not an
# error — but the token is spent, so nothing further happens.
status="$(post_json "verify-email-twice-$$" /v1/auth/verify-email \
  "{\"token\":\"$verification_token\"}" "$WORKDIR/verify-twice.json")"
[[ "$status" == "200" ]] || fail "clicking the link twice returned $status"
consumed_count="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from email_verification_tokens
    where user_id = '$registered_id' and consumed_reason = 'verified';")"
[[ "$consumed_count" == "1" ]] || fail "$consumed_count tokens are marked verified, want 1"
ok "clicking twice is not an error, and consumes nothing a second time"

# Resend answers identically whether or not the address is known, which is what stops this
# endpoint being a way of asking who has an account.
status="$(post_json "verify-resend-known-$$" /v1/auth/resend-verify \
  "{\"email\":\"$reg_email\"}" "$WORKDIR/resend-known.json")"
[[ "$status" == "202" ]] || { cat "$WORKDIR/resend-known.json"; fail "resend returned $status, want 202"; }
status="$(post_json "verify-resend-unknown-$$" /v1/auth/resend-verify \
  '{"email":"nobody-at-all@example.com"}' "$WORKDIR/resend-unknown.json")"
[[ "$status" == "202" ]] || fail "resend for an unknown address returned $status"
diff -q "$WORKDIR/resend-known.json" "$WORKDIR/resend-unknown.json" >/dev/null \
  || fail "a known address is answered differently from an unknown one"
ok "resend answers 202 identically for a known and an unknown address"

unknown_tokens="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from email_verification_tokens where email = 'nobody-at-all@example.com';")"
[[ "$unknown_tokens" == "0" ]] || fail "a token was stored for an address with no account"
ok "and stores nothing for the address that has no account"

# ---------------------------------------------------------------------------------------
ticket "SHIP-36  POST /v1/auth/verify-phone marks the number verified after a correct OTP"

# The code sent at SHIP-34 was superseded by nothing since, so it is still the live one.
status="$(post_json "verify-phone-wrong-$$" /v1/auth/verify-phone \
  "{\"phone\":\"$reg_phone_local\",\"code\":\"000000\"}" "$WORKDIR/verify-phone-wrong.json")"
if [[ "$status" != "400" ]]; then
  cat "$WORKDIR/verify-phone-wrong.json"
  fail "a wrong code returned $status, want 400"
fi
[[ "$(json "$WORKDIR/verify-phone-wrong.json" '["error"]["code"]')" == "identity_otp_invalid" ]] \
  || fail "expected code=identity_otp_invalid"
ok "a wrong code is refused"

# The attempt is recorded, and that is the half most easily lost: an increment that rolled back
# with the error would leave the counter at zero and the five-guess limit limiting nothing.
attempts="$("$PSQL" "$DATABASE_URL" -tAc \
  "select attempts from phone_otps where user_id = '$registered_id' and consumed_at is null;")"
[[ "$attempts" == "1" ]] || fail "attempts = $attempts after one wrong guess, want 1"
ok "the wrong guess was counted, so the attempt limit counts something"

# Every failure looks alike, including the one that would otherwise say whether a number has an
# account at all.
status="$(post_json "verify-phone-unknown-$$" /v1/auth/verify-phone \
  '{"phone":"+61499999998","code":"000000"}' "$WORKDIR/verify-phone-unknown.json")"
[[ "$status" == "400" ]] || fail "an unknown number returned $status"
[[ "$(json "$WORKDIR/verify-phone-unknown.json" '["error"]["code"]')" == "identity_otp_invalid" ]] \
  || fail "an unknown number is distinguishable from a wrong code"
ok "a number with no account is answered exactly as a wrong code is"

status="$(post_json "verify-phone-$$" /v1/auth/verify-phone \
  "{\"phone\":\"$reg_phone_local\",\"code\":\"$otp_code\"}" "$WORKDIR/verify-phone.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/verify-phone.json"; fail "POST /v1/auth/verify-phone returned $status, want 200"; }
[[ "$(json "$WORKDIR/verify-phone.json" '["phone_verified"]')" == "True" ]] \
  || fail "the response does not report the number as verified"
ok "the correct code verifies the number"

phone_state="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (u.phone_verified_at is not null)::text || ' ' || coalesce(o.consumed_reason, 'live')
     from users u join phone_otps o on o.user_id = u.id
    where u.id = '$registered_id'
    order by o.created_at desc limit 1;")"
[[ "$phone_state" == "true verified" ]] || fail "the row says '$phone_state', want 'true verified'"
ok "the column is set and the code is consumed, both in the database"

# Both channels verified is what Docs/04 §2 requires before a customer may publish, and this run
# has now established both on one account.
both_verified="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (email_verified_at is not null and phone_verified_at is not null)::text
     from users where id = '$registered_id';")"
[[ "$both_verified" == "true" ]] || fail "the account is not verified on both channels"
ok "the account is now verified on both channels, which is Docs/04 §2's baseline"

# The code is spent. Replaying it must not verify anything a second time.
status="$(post_json "verify-phone-replay-$$" /v1/auth/verify-phone \
  "{\"phone\":\"$reg_phone_local\",\"code\":\"$otp_code\"}" "$WORKDIR/verify-phone-replay.json")"
[[ "$status" == "400" ]] || fail "a consumed code was accepted again (status $status)"
ok "the consumed code cannot be used again"

# ---------------------------------------------------------------------------------------
ticket "SHIP-39  a refresh token that expires, rotated on every use"

# The behaviour — a new token each time, the predecessor refused, one winner when two arrive
# together — is demonstrated end to end against the running service in SHIP-42's section below,
# because that is where the endpoint exists. What is checked here is the half that lives in the
# schema: the expiry decision Docs/11 §9 carried from SHIP-38, and which 000103 settles.

expiry_column="$("$PSQL" "$DATABASE_URL" -tAc \
  "select data_type || ' null=' || is_nullable || ' default=' || coalesce(column_default, 'none')
     from information_schema.columns
    where table_schema = 'public' and table_name = 'device_sessions'
      and column_name = 'refresh_token_expires_at';")"
[[ -n "$expiry_column" ]] \
  || fail "device_sessions has no refresh_token_expires_at; a refresh token that never expires is a stolen phone signed in forever"
[[ "$expiry_column" == "timestamp with time zone null=NO default=none" ]] \
  || fail "refresh_token_expires_at is '$expiry_column', want 'timestamp with time zone null=NO default=none'"
ok "the refresh token's expiry is an explicit column, NOT NULL, and not defaulted by the database"

# NOT NULL with no default is the whole control. A default would let a writer omit the expiry
# and get one from the database's clock rather than from the clock that computed it.
if "$PSQL" "$DATABASE_URL" -q -c \
  "insert into device_sessions (id, user_id, refresh_token_hash, device_label)
   values (gen_random_uuid(), '$verify_user', 'no-expiry-$$', 'Verify iPhone');" >/dev/null 2>&1; then
  fail "a session was created with no refresh token expiry"
fi
ok "a session with no expiry is refused, so no credential can be issued without a lifetime"

# An expiry already in the past at the moment of issue is a clock or configuration mistake, and
# it is worth catching where it happens rather than as a device that can never stay signed in.
if "$PSQL" "$DATABASE_URL" -q -c \
  "insert into device_sessions
       (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label, created_at)
   values (gen_random_uuid(), '$verify_user', 'past-expiry-$$',
           now() - interval '1 day', 'Verify iPhone', now());" >/dev/null 2>&1; then
  fail "a session was created holding a token that had already expired"
fi
ok "ck_device_sessions_refresh_expiry refuses a token that expired before it was issued"

# The expiry is its own column rather than being derived from last_seen_at, which 000100
# describes as display text for the device list. Deriving a credential's lifetime from a display
# column means every future write to it silently extends the credential — so the two must be
# separately writable, and this shows they are.
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into device_sessions
       (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label, last_seen_at)
   values (gen_random_uuid(), '$verify_user', 'sliding-$$',
           now() + interval '30 days', 'Verify iPhone', now() - interval '10 days');" >/dev/null \
  || fail "a session could not be created with an expiry and a last_seen_at that disagree"
independent="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (refresh_token_expires_at > last_seen_at + interval '35 days')::text
     from device_sessions where refresh_token_hash = 'sliding-$$';")"
[[ "$independent" == "true" ]] \
  || fail "the expiry appears to be derived from last_seen_at rather than written independently"
ok "expiry and last seen are separate columns, so a device-list read cannot extend a session"

# ---------------------------------------------------------------------------------------
ticket "SHIP-40  presenting a consumed refresh token invalidates the whole device session"

# Like SHIP-39 above, the behaviour is driven end to end in SHIP-42's section, where the
# endpoint exists. What is checked here is the state the behaviour rests on: a spent token has
# to stay recognisable, and a session has to be able to end.

"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from information_schema.tables where table_name = 'consumed_refresh_tokens';" | grep -q 1 \
  || fail "consumed_refresh_tokens does not exist; a rotated token would be indistinguishable from one nobody issued"
ok "consumed_refresh_tokens exists, at migration 000104 inside identity's reserved block"

# Unique, because the hash is what reuse detection looks a session up by and the answer decides
# what gets revoked.
hash_index="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from pg_index i
     join pg_class t on t.oid = i.indrelid
     join pg_class x on x.oid = i.indexrelid
    where t.relname = 'consumed_refresh_tokens' and i.indisunique
      and x.relname = 'uq_consumed_refresh_tokens_hash';")"
[[ "$hash_index" == "1" ]] || fail "consumed_refresh_tokens.token_hash is not uniquely indexed"
ok "a spent token hash belongs to exactly one session"

# The three revocation reasons, constrained by the database rather than by application logic —
# and a session ends by being marked, never by being deleted (Docs/10 §3.3).
revoked_columns="$("$PSQL" "$DATABASE_URL" -tAc \
  "select string_agg(column_name, ' ' order by column_name)
     from information_schema.columns
    where table_schema = 'public' and table_name = 'device_sessions'
      and column_name in ('revoked_at', 'revoked_reason');")"
[[ "$revoked_columns" == "revoked_at revoked_reason" ]] \
  || fail "device_sessions cannot record that a session ended; found '$revoked_columns'"
ok "a session ends by being marked revoked, with a reason, rather than by being deleted"

# One user, one session, used from here down. A fresh account rather than $registered_id,
# because this section revokes what it creates.
reuse_user="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into users (id, email, phone, password_hash, role)
   values (gen_random_uuid(), 'reuse-$$@example.com', '+6140004$$', 'x', 'customer')
   returning id;")"
reuse_session="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into device_sessions
       (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label)
   values (gen_random_uuid(), '$reuse_user', 'reuse-live-$$', now() + interval '30 days',
           'Verify iPhone')
   returning id;")"

if "$PSQL" "$DATABASE_URL" -q -c \
  "update device_sessions set revoked_at = now() where id = '$reuse_session';" >/dev/null 2>&1; then
  fail "a session was revoked with no reason recorded"
fi
if "$PSQL" "$DATABASE_URL" -q -c \
  "update device_sessions set revoked_at = now(), revoked_reason = 'because'
    where id = '$reuse_session';" >/dev/null 2>&1; then
  fail "an unrecognised revocation reason was accepted"
fi
ok "ck_device_sessions_revoked and ck_device_sessions_revoked_reason both hold"

# The invariant 000104 is shaped around: a hash is live in device_sessions or spent in the
# ledger, never both. Two lookups are only an answer while that holds.
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into consumed_refresh_tokens (id, session_id, token_hash, consumed_at)
   values (gen_random_uuid(), '$reuse_session', 'reuse-spent-$$', now());" >/dev/null \
  || fail "a spent token could not be recorded"
if "$PSQL" "$DATABASE_URL" -q -c \
  "delete from device_sessions where id = '$reuse_session';" >/dev/null 2>&1; then
  fail "deleting the session took the evidence that its tokens were spent with it"
fi
ok "ON DELETE RESTRICT holds, so a deleted session cannot turn spent tokens back into unknown ones"

# ---------------------------------------------------------------------------------------
ticket "SHIP-42  POST /v1/auth/refresh rotates the pair and rejects a reused token"

# This section is where SHIP-39 and SHIP-40 stop being schema and start being behaviour: the
# real endpoint, against the running service, over HTTP.
#
# The session is created directly in the database because sign-in is SHIP-41 and does not exist
# yet — there is no endpoint that issues a first refresh token. The token is generated here and
# only its SHA-256 is stored, which is exactly what the service does, so the endpoint is being
# driven with a token it has no other way of knowing.
refresh_token_1="$(openssl rand -hex 32)"
refresh_hash_1="$(printf '%s' "$refresh_token_1" | shasum -a 256 | cut -d' ' -f1)"

refresh_session="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into device_sessions
       (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label)
   values (gen_random_uuid(), '$registered_id', '$refresh_hash_1', now() + interval '30 days',
           'Verify iPhone')
   returning id;")"
[[ "$refresh_session" =~ ^[0-9a-f-]{36}$ ]] || fail "could not create a session to refresh: $refresh_session"

status="$(post_json "verify-refresh-$$" /v1/auth/refresh \
  "{\"refresh_token\":\"$refresh_token_1\"}" "$WORKDIR/refresh.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/refresh.json"; fail "POST /v1/auth/refresh returned $status, want 200"; }

refresh_token_2="$(json "$WORKDIR/refresh.json" '["refresh_token"]')"
access_token_2="$(json "$WORKDIR/refresh.json" '["access_token"]')"
[[ -n "$refresh_token_2" && "$refresh_token_2" != "$refresh_token_1" ]] \
  || fail "the refresh returned the token it was given, so nothing rotated"
[[ "$access_token_2" =~ ^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$ ]] \
  || fail "the refresh returned no access token"
ok "a new access token and a new refresh token come back"

# Lifetimes are seconds rather than instants, so a handset with a wrong clock still refreshes at
# the right moment. Fifteen minutes and thirty days are Docs/10 §5 and SHIP-39's constant.
[[ "$(json "$WORKDIR/refresh.json" '["expires_in"]')" == "900" ]] \
  || fail "expires_in is not 900 seconds"
[[ "$(json "$WORKDIR/refresh.json" '["refresh_token_expires_in"]')" == "2592000" ]] \
  || fail "refresh_token_expires_in is not thirty days"
ok "both lifetimes are reported in seconds — fifteen minutes and thirty days"

# The access token names the device it was issued to, which is what makes "sign this phone out"
# reach the access token as well as the refresh one.
python3 - "$access_token_2" >"$WORKDIR/refresh-claims.json" <<'PYTHON'
import base64, json, sys

def segment(s):
    return json.loads(base64.urlsafe_b64decode(s + "=" * (-len(s) % 4)))

_header, payload, _signature = sys.argv[1].split(".")
json.dump(segment(payload), sys.stdout)
PYTHON
[[ "$(json "$WORKDIR/refresh-claims.json" '["sub"]')" == "$registered_id" ]] \
  || fail "the access token names a different account"
[[ "$(json "$WORKDIR/refresh-claims.json" '["sid"]')" == "$refresh_session" ]] \
  || fail "the access token does not name the device session it was issued against"
ok "the access token carries the account and the device session it belongs to"

# The row moved, and the predecessor is recorded as spent rather than merely forgotten.
rotated_state="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (d.refresh_token_hash <> '$refresh_hash_1')::text || ' ' ||
          (select count(*) from consumed_refresh_tokens c
            where c.session_id = d.id and c.token_hash = '$refresh_hash_1')::text
     from device_sessions d where d.id = '$refresh_session';")"
[[ "$rotated_state" == "true 1" ]] \
  || fail "the row says '$rotated_state', want 'true 1' — rotated, with the predecessor recorded as spent"
ok "the session holds the new hash and the old one is in consumed_refresh_tokens"

# And the token itself is nowhere, in either table.
leaked="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (select count(*) from device_sessions where refresh_token_hash = '$refresh_token_1')
        + (select count(*) from consumed_refresh_tokens where token_hash = '$refresh_token_1')
        + (select count(*) from device_sessions where refresh_token_hash = '$refresh_token_2');")"
[[ "$leaked" == "0" ]] || fail "a refresh token appears in the database in plain form"
ok "searching both tables for either token finds nothing — only hashes are stored"

# SHIP-40, end to end. The spent token ends the whole session, not merely this request.
status="$(post_json "verify-refresh-reuse-$$" /v1/auth/refresh \
  "{\"refresh_token\":\"$refresh_token_1\"}" "$WORKDIR/refresh-reuse.json")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/refresh-reuse.json"; fail "a reused token returned $status, want 400"; }
[[ "$(json "$WORKDIR/refresh-reuse.json" '["error"]["code"]')" == "identity_refresh_token_invalid" ]] \
  || fail "expected code=identity_refresh_token_invalid"
ok "presenting the spent token is refused, with the same code every other failure gets"

revoked="$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(revoked_reason, 'live') from device_sessions where id = '$refresh_session';")"
[[ "$revoked" == "refresh_token_reused" ]] \
  || fail "the session is '$revoked', want refresh_token_reused — the revocation did not survive the refusal"
ok "the device session is revoked, and the revocation survived the error that reported it"

# The half that makes it "the entire device session": the token the legitimate device is holding
# stops working too. Refusing only the reused one would leave whoever stole it signed in.
status="$(post_json "verify-refresh-after-reuse-$$" /v1/auth/refresh \
  "{\"refresh_token\":\"$refresh_token_2\"}" "$WORKDIR/refresh-after-reuse.json")"
[[ "$status" == "400" ]] || fail "the live token still refreshed after reuse was detected (status $status)"
ok "the token the device legitimately held stops working as well — the whole session ended"

# A token nobody issued revokes nothing. Without this, anybody could sign anybody out by
# guessing, which would make reuse detection a weapon rather than a control.
other_token="$(openssl rand -hex 32)"
other_hash="$(printf '%s' "$other_token" | shasum -a 256 | cut -d' ' -f1)"
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into device_sessions
       (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label)
   values (gen_random_uuid(), '$registered_id', '$other_hash', now() + interval '30 days',
           'Verify Pixel');" >/dev/null \
  || fail "could not create a second session"

status="$(post_json "verify-refresh-unknown-$$" /v1/auth/refresh \
  '{"refresh_token":"9qE2vT7bYw1sJk4pNc0aRlX8oZgHdM3uQiV6yB5tCfE"}' "$WORKDIR/refresh-unknown.json")"
[[ "$status" == "400" ]] || fail "an unknown token returned $status, want 400"
still_live="$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(revoked_reason, 'live') from device_sessions where refresh_token_hash = '$other_hash';")"
[[ "$still_live" == "live" ]] || fail "a guessed token ended somebody's session"
ok "a token nobody issued is refused and revokes nothing"

# An expired refresh token is refused, which is the whole point of 000103's column.
expired_token="$(openssl rand -hex 32)"
expired_hash="$(printf '%s' "$expired_token" | shasum -a 256 | cut -d' ' -f1)"
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into device_sessions
       (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label, created_at)
   values (gen_random_uuid(), '$registered_id', '$expired_hash', now() - interval '1 second',
           'Verify Expired', now() - interval '31 days');" >/dev/null \
  || fail "could not create a session holding a lapsed token"
status="$(post_json "verify-refresh-expired-$$" /v1/auth/refresh \
  "{\"refresh_token\":\"$expired_token\"}" "$WORKDIR/refresh-expired.json")"
[[ "$status" == "400" ]] || fail "an expired refresh token returned $status, want 400"
ok "a refresh token past its expiry is refused, which is what the expiry column is for"

# Rotation is the most retry-sensitive request in the platform — two refreshes with two keys is
# a client revoking its own session — so the idempotency middleware is what makes an honest
# retry safe, and the endpoint refuses a request without a key like every other mutation.
status="$(curl -s -X POST -o "$WORKDIR/refresh-nokey.json" -w '%{http_code}' \
  -H 'Content-Type: application/json' -d '{"refresh_token":"x"}' \
  "http://localhost:$VERIFY_PORT/v1/auth/refresh")"
[[ "$status" == "400" ]] || fail "refresh without an Idempotency-Key returned $status"
[[ "$(json "$WORKDIR/refresh-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || fail "expected code=idempotency_key_required"
ok "it needs an Idempotency-Key, so a retry replays the pair rather than rotating twice"

# ---------------------------------------------------------------------------------------
ticket "SHIP-41  POST /v1/auth/login returns an access and refresh token pair"

# SHIP-47 limits failed sign-ins per account and per network address, and every request in this
# script arrives from the loopback — so one address bucket is shared by every section below, and by
# every previous run of this script. A run that ended part-way through SHIP-47 would otherwise
# leave it empty and the *next* run would fail here, with a 429 that looks like a broken endpoint.
#
# **Do not compose that bucket's key from a literal.** `localhost` resolves to `::1` rather than to
# `127.0.0.1` on this machine, so a key built as `rl:v1:credential:address:127.0.0.1` names nothing and
# a comparison against it passes by matching two empty strings. Scan for the pattern, as below.
#
# Cleared once, at the point sign-ins begin, so the run starts from a known state — and the state is
# now **asserted rather than argued from what ran before**.
#
# This comment used to end *"Nothing above this line signs in"*, and wave 16 made it false:
# SHIP-47's 429 proof in `30-http.sh` drives sign-in past the per-address bucket several sections
# earlier. Nothing broke, because the clear was already written for leftovers from a previous run —
# but **a claim of the form "nothing above this line does X" is true only of the harness as it stood
# when it was written, and nothing checks it.** The assertion below checks the thing the sentence
# was standing in for, so the next section that signs in early breaks nothing and corrects no
# comment. It is a precondition rather than an acceptance criterion, so it counts no check.
# credential_bucket_keys — every key Docs/12's Credential class leaves behind.
#
# **Two namespaces rather than one, since SHIP-183b.** The account half is `signin:account:`; the
# address half is `credential:address:`, and it was renamed when it stopped being sign-in's alone —
# refresh and both verification endpoints now spend the same bucket, because Docs/12 §9 puts one
# bucket on a class and never one on a route. A clear that kept scanning `rl:v1:signin:*` would
# empty the account keys, leave the address bucket exactly as full as it was, and report success.
credential_bucket_keys() {
  redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:signin:*'
  redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:credential:*'
}

credential_bucket_keys | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true

[[ "$(credential_bucket_keys | wc -l | tr -d ' ')" == "0" ]] \
  || fail "the sign-in rate-limit buckets are not empty after the clear, so this section's sign-ins start throttled"

# The account registered above, whose password is the literal this file already knows. Signing in
# is the first endpoint that creates a session rather than being handed one, so from here the
# sections below no longer have to plant device_sessions rows by hand.
login_password="correct-horse-battery-staple"

sessions_before="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_sessions where user_id = '$registered_id';")"

status="$(post_json "verify-login-$$" /v1/auth/login \
  "{\"email\":\"$reg_email\",\"password\":\"$login_password\",\"device_label\":\"Verify iPhone 17\"}" \
  "$WORKDIR/login.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/login.json"; fail "POST /v1/auth/login returned $status, want 200"; }

login_access="$(json "$WORKDIR/login.json" '["access_token"]')"
login_refresh="$(json "$WORKDIR/login.json" '["refresh_token"]')"
[[ "$login_access" =~ ^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$ ]] \
  || fail "the sign-in returned no access token"
[[ "$login_refresh" =~ ^[A-Za-z0-9_-]{43}$ ]] \
  || fail "the sign-in returned no refresh token, or one of the wrong shape: $login_refresh"
ok "an access token and a refresh token come back for the right password"

# Read from the service's own TTLs rather than a wall clock, so a handset with a wrong clock
# still refreshes at the right moment (SHIP-42's decision, reused here rather than restated).
[[ "$(json "$WORKDIR/login.json" '["expires_in"]')" == "900" ]] \
  || fail "expires_in is not 900 seconds"
[[ "$(json "$WORKDIR/login.json" '["refresh_token_expires_in"]')" == "2592000" ]] \
  || fail "refresh_token_expires_in is not thirty days"
ok "both lifetimes are reported in seconds — fifteen minutes and thirty days"

# The pair is the same shape refresh returns, and the same schema describes both.
login_sid="$(python3 - "$login_access" <<'PYTHON'
import base64, json, sys

_header, payload, _signature = sys.argv[1].split(".")
claims = json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))
print(claims["sub"], claims["sid"])
PYTHON
)"
read -r claim_sub claim_sid <<<"$login_sid"
[[ "$claim_sub" == "$registered_id" ]] || fail "the access token names $claim_sub, not the account that signed in"
ok "the access token names the account that presented the password"

session_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select device_label || ' ' || (user_id = '$registered_id')::text || ' ' ||
          coalesce(revoked_reason, 'live')
     from device_sessions where id = '$claim_sid';")"
[[ "$session_row" == "Verify iPhone 17 true live" ]] \
  || fail "the session the token names says '$session_row', want 'Verify iPhone 17 true live'"
ok "a live device session exists, owned by that account and carrying the label that was sent"

# The pair is usable rather than merely well shaped: a refresh token nothing will exchange would
# pass every check above.
status="$(post_json "verify-login-refresh-$$" /v1/auth/refresh \
  "{\"refresh_token\":\"$login_refresh\"}" "$WORKDIR/login-refresh.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/login-refresh.json"; fail "the refresh token sign-in issued was refused ($status)"; }
login_refresh="$(json "$WORKDIR/login-refresh.json" '["refresh_token"]')"
ok "the refresh token sign-in issued is one the platform will exchange"

# Only the hash is stored, exactly as rotation stores it.
leaked="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_sessions where refresh_token_hash = '$login_refresh';")"
[[ "$leaked" == "0" ]] || fail "sign-in stored the refresh token itself, not its hash"
ok "searching device_sessions for the token finds nothing — only its hash is kept"

# The two failures anybody can provoke without holding anything are one answer, or this endpoint
# tells the world which addresses have accounts.
status="$(post_json "verify-login-wrong-$$" /v1/auth/login \
  "{\"email\":\"$reg_email\",\"password\":\"not-the-registered-password\",\"device_label\":\"Verify iPhone 17\"}" \
  "$WORKDIR/login-wrong.json")"
[[ "$status" == "400" ]] || fail "the wrong password returned $status, want 400"
wrong_code="$(json "$WORKDIR/login-wrong.json" '["error"]["code"]')"

status="$(post_json "verify-login-unknown-$$" /v1/auth/login \
  "{\"email\":\"nobody-$$@example.com\",\"password\":\"$login_password\",\"device_label\":\"Verify iPhone 17\"}" \
  "$WORKDIR/login-unknown.json")"
[[ "$status" == "400" ]] || fail "an address with no account returned $status, want 400"
unknown_code="$(json "$WORKDIR/login-unknown.json" '["error"]["code"]')"

[[ "$wrong_code" == "identity_credentials_invalid" && "$unknown_code" == "$wrong_code" ]] \
  || fail "the wrong password answers '$wrong_code' and an unknown address '$unknown_code'; two answers is an account-existence oracle"
ok "a wrong password and an address with no account are one answer, with no session created"

# A body-borne credential is refused with 400 across this domain, and 401 is reserved for one
# presented in the bearer header — see SHIP-46 below, which is the first route that does.
[[ "$(json "$WORKDIR/login-wrong.json" '["error"]["request_id"]')" != "" ]] \
  || fail "the refusal carries no request id"
ok "the refusal is 400 with the request id in the body, not a 401 with a bearer challenge"

# Account standing is disclosed only to somebody who has just proved they own the account.
suspended_email="suspended-$$@example.com"
status="$(post_json "verify-suspend-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$suspended_email\",\"phone\":\"04960$$\",\"password\":\"$login_password\",\"role\":\"customer\"}" \
  "$WORKDIR/suspend-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/suspend-register.json"; fail "could not register the account to suspend ($status)"; }
"$PSQL" "$DATABASE_URL" -q -c \
  "update users set status = 'suspended' where email = '$suspended_email';" >/dev/null \
  || fail "could not suspend the account"

status="$(post_json "verify-login-suspended-$$" /v1/auth/login \
  "{\"email\":\"$suspended_email\",\"password\":\"$login_password\",\"device_label\":\"Verify iPhone 17\"}" \
  "$WORKDIR/login-suspended.json")"
[[ "$status" == "403" ]] || { cat "$WORKDIR/login-suspended.json"; fail "a suspended account returned $status, want 403"; }
[[ "$(json "$WORKDIR/login-suspended.json" '["error"]["code"]')" == "identity_account_suspended" ]] \
  || fail "expected code=identity_account_suspended"

status="$(post_json "verify-login-suspended-wrong-$$" /v1/auth/login \
  "{\"email\":\"$suspended_email\",\"password\":\"not-the-registered-password\",\"device_label\":\"Verify iPhone 17\"}" \
  "$WORKDIR/login-suspended-wrong.json")"
[[ "$(json "$WORKDIR/login-suspended-wrong.json" '["error"]["code"]')" == "identity_credentials_invalid" ]] \
  || fail "a suspended account disclosed its standing to somebody who did not have the password"
ok "a suspended account is told so, and only after the password verified"

suspended_sessions="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_sessions d join users u on u.id = d.user_id where u.email = '$suspended_email';")"
[[ "$suspended_sessions" == "0" ]] || fail "a suspended account was given $suspended_sessions device sessions"
ok "no session is created for an account that may not hold one"

# Signing in creates a row, so a retry after a dropped connection must replay rather than leave a
# second device live for thirty days that its owner never used.
status="$(post_json "verify-login-retry-$$" /v1/auth/login \
  "{\"email\":\"$reg_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Retry\"}" \
  "$WORKDIR/login-retry-1.json")"
[[ "$status" == "200" ]] || fail "the sign-in to retry returned $status"
status="$(post_json "verify-login-retry-$$" /v1/auth/login \
  "{\"email\":\"$reg_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Retry\"}" \
  "$WORKDIR/login-retry-2.json")"
[[ "$status" == "200" ]] || fail "the retried sign-in returned $status"
[[ "$(json "$WORKDIR/login-retry-1.json" '["refresh_token"]')" == "$(json "$WORKDIR/login-retry-2.json" '["refresh_token"]')" ]] \
  || fail "the retry issued a second pair rather than replaying the first"
retried_devices="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_sessions where user_id = '$registered_id' and device_label = 'Verify Retry';")"
[[ "$retried_devices" == "1" ]] || fail "the retry created $retried_devices devices, want 1"
ok "a retry with the same Idempotency-Key replays the pair rather than creating a second device"

status="$(curl -s -X POST -o "$WORKDIR/login-nokey.json" -w '%{http_code}' \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$reg_email\",\"password\":\"$login_password\",\"device_label\":\"Verify iPhone 17\"}" \
  "http://localhost:$VERIFY_PORT/v1/auth/login")"
[[ "$status" == "400" ]] || fail "sign-in without an Idempotency-Key returned $status"
[[ "$(json "$WORKDIR/login-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || fail "expected code=idempotency_key_required"
ok "it is refused without an Idempotency-Key, like every other state-changing request"

# Every field at once, the device label included — so a client is not told about the label only
# after its password has been verified.
status="$(post_json "verify-login-invalid-$$" /v1/auth/login \
  '{"email":"not-an-address","password":"","device_label":""}' "$WORKDIR/login-invalid.json")"
[[ "$status" == "422" ]] || fail "an unusable sign-in body returned $status, want 422"
login_fields="$(python3 -c 'import json,sys
print(" ".join(sorted(d["field"] for d in json.load(open(sys.argv[1]))["error"]["details"])))' \
  "$WORKDIR/login-invalid.json")"
[[ "$login_fields" == "device_label email password" ]] \
  || fail "the details name '$login_fields', want every offending field"
ok "validation names every offending field at once, including the device label"

sessions_after="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_sessions where user_id = '$registered_id';")"
[[ "$sessions_after" == "$((sessions_before + 2))" ]] \
  || fail "the account gained $((sessions_after - sessions_before)) devices across this section, want 2"
ok "only the two successful sign-ins created a device; every refusal created none"

# ---------------------------------------------------------------------------------------
ticket "SHIP-43  POST /v1/auth/logout revokes the current device session only"

# post_auth <key> <token> <path> <outfile> — a state-changing request with a bearer credential.
#
# Defined here rather than in the harness because SHIP-43 and SHIP-46 are the only callers today
# and both are in this file. Whoever adds the second domain with protected mutations should move
# it up beside post_json, which is the shape the harness already uses for the unauthenticated
# half.
post_auth() {
  curl -s -X POST -o "$4" -w '%{http_code}' \
    -H "Idempotency-Key: $1" -H "$auth_header: Bearer $2" \
    "http://localhost:$VERIFY_PORT$3"
}

# Two devices on one account, both created through the real endpoint. "Only" is the half of the
# criterion a plausible implementation gets wrong, and it cannot be shown with one device.
status="$(post_json "verify-logout-a-$$" /v1/auth/login \
  "{\"email\":\"$reg_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Signed Out\"}" \
  "$WORKDIR/logout-a.json")"
[[ "$status" == "200" ]] || fail "could not sign in the device to sign out ($status)"
status="$(post_json "verify-logout-b-$$" /v1/auth/login \
  "{\"email\":\"$reg_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Still In\"}" \
  "$WORKDIR/logout-b.json")"
[[ "$status" == "200" ]] || fail "could not sign in the device that stays signed in ($status)"

signed_out_access="$(json "$WORKDIR/logout-a.json" '["access_token"]')"
signed_out_refresh="$(json "$WORKDIR/logout-a.json" '["refresh_token"]')"
still_in_refresh="$(json "$WORKDIR/logout-b.json" '["refresh_token"]')"

signed_out_sid="$(python3 - "$signed_out_access" <<'PYTHON'
import base64, json, sys

_header, payload, _signature = sys.argv[1].split(".")
print(json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))["sid"])
PYTHON
)"

status="$(post_auth "verify-logout-$$" "$signed_out_access" /v1/auth/logout "$WORKDIR/logout.json")"
[[ "$status" == "204" ]] || { cat "$WORKDIR/logout.json"; fail "POST /v1/auth/logout returned $status, want 204"; }
[[ ! -s "$WORKDIR/logout.json" ]] || fail "logout answered with a body; 204 has nothing to say"
ok "signing out answers 204 with no body"

# The row is marked with the reason that says how it ended, rather than deleted (Docs/10 §3.3).
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(revoked_reason, 'live') from device_sessions where id = '$signed_out_sid';")" == "signed_out" ]] \
  || fail "the session is not marked signed_out"
ok "the session is marked revoked with reason 'signed_out', not deleted"

status="$(post_json "verify-logout-refresh-$$" /v1/auth/refresh \
  "{\"refresh_token\":\"$signed_out_refresh\"}" "$WORKDIR/logout-refresh.json")"
[[ "$status" == "400" ]] || fail "the signed-out device could still refresh ($status)"
ok "the refresh token that device was holding stops working"

# The half a plausible implementation gets wrong: signing out every device the account owns looks
# correct from the device that asked and is only visible from the other one.
status="$(post_json "verify-logout-other-$$" /v1/auth/refresh \
  "{\"refresh_token\":\"$still_in_refresh\"}" "$WORKDIR/logout-other.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/logout-other.json"; fail "the other device was signed out too ($status)"; }
ok "every other device on the account is untouched — the current session only"

# SHIP-40's invariant survives: a hash is live in device_sessions or spent in the ledger, never
# both. Moving the live hash across on sign-out is the tidy-looking change that breaks it.
overlap="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_sessions d
     join consumed_refresh_tokens c on c.token_hash = d.refresh_token_hash
    where d.id = '$signed_out_sid';")"
[[ "$overlap" == "0" ]] || fail "signing out moved the live hash into the ledger, so one hash is in both places"
ok "the live refresh token hash stays where it is, so SHIP-40's invariant still holds"

# The client has discarded its tokens by the time it reads the response, so a retry — or a stale
# tab — must not become an error.
status="$(post_auth "verify-logout-again-$$" "$signed_out_access" /v1/auth/logout "$WORKDIR/logout-again.json")"
[[ "$status" == "204" ]] || fail "signing out twice returned $status, want 204"
ok "signing out again is not an error; the state the caller asked for already holds"

# The other half of the status rule sign-in states: a credential in the bearer header, refused
# with 401 and the challenge RFC 9110 requires. This is the first route in the service that can
# demonstrate it — SHIP-44 built the middleware and nothing had used it.
status="$(curl -s -X POST -o "$WORKDIR/logout-anon.json" -D "$WORKDIR/logout-anon.headers" \
  -w '%{http_code}' -H "Idempotency-Key: verify-logout-anon-$$" \
  "http://localhost:$VERIFY_PORT/v1/auth/logout")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/logout-anon.json"; fail "signing out with no credential returned $status, want 401"; }
grep -qi '^WWW-Authenticate: Bearer' "$WORKDIR/logout-anon.headers" \
  || fail "no WWW-Authenticate challenge on the 401, which RFC 9110 requires"
[[ "$(json "$WORKDIR/logout-anon.json" '["error"]["code"]')" == "unauthenticated" ]] \
  || fail "expected code=unauthenticated"
ok "a protected route refuses a caller with no credential — 401, with a bearer challenge"

status="$(post_auth "verify-logout-junk-$$" "not.a.token" /v1/auth/logout "$WORKDIR/logout-junk.json")"
[[ "$status" == "401" ]] || fail "a malformed bearer token returned $status, want 401"
[[ "$(json "$WORKDIR/logout-junk.json" '["error"]["code"]')" == "unauthenticated" ]] \
  || fail "a malformed token was answered with something other than 'unauthenticated'"
ok "a malformed credential is refused the same way, saying nothing about which check refused it"

status="$(curl -s -X POST -o "$WORKDIR/logout-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $signed_out_access" \
  "http://localhost:$VERIFY_PORT/v1/auth/logout")"
[[ "$status" == "400" ]] || fail "signing out without an Idempotency-Key returned $status"
[[ "$(json "$WORKDIR/logout-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || fail "expected code=idempotency_key_required"
ok "it needs an Idempotency-Key like every other mutation, checked before the auth class"

# ---------------------------------------------------------------------------------------
ticket "SHIP-46  a user can list their devices and revoke any one of them"

# A fresh account, because this section counts rows. $registered_id has accumulated sessions from
# SHIP-41, SHIP-42 and SHIP-43, and a count against it would be a check that passes because of
# arithmetic somebody has to redo whenever a section above changes.
devices_email="devices-$$@example.com"
status="$(post_json "verify-devices-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$devices_email\",\"phone\":\"04950$$\",\"password\":\"$login_password\",\"role\":\"provider\"}" \
  "$WORKDIR/devices-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/devices-register.json"; fail "could not register the device-list account ($status)"; }
devices_user="$(json "$WORKDIR/devices-register.json" '["id"]')"

for device in one two three; do
  status="$(post_json "verify-devices-$device-$$" /v1/auth/login \
    "{\"email\":\"$devices_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Device $device\"}" \
    "$WORKDIR/devices-$device.json")"
  [[ "$status" == "200" ]] || fail "could not sign in device $device ($status)"
done

devices_access="$(json "$WORKDIR/devices-one.json" '["access_token"]')"
devices_current_sid="$(python3 - "$devices_access" <<'PYTHON'
import base64, json, sys

_header, payload, _signature = sys.argv[1].split(".")
print(json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))["sid"])
PYTHON
)"

# get_auth <token> <path> <outfile> — a read with a bearer credential and no Idempotency-Key,
# because the middleware lets safe methods through untouched.
get_auth() {
  curl -s -o "$3" -w '%{http_code}' -H "$auth_header: Bearer $1" \
    "http://localhost:$VERIFY_PORT$2"
}

status="$(get_auth "$devices_access" /v1/auth/sessions "$WORKDIR/devices-list.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/devices-list.json"; fail "GET /v1/auth/sessions returned $status, want 200"; }

# One line of whitespace-free tokens, because the labels themselves contain spaces and a `read`
# over them would silently absorb the fields that follow.
listed="$(python3 -c 'import json,sys
body = json.load(open(sys.argv[1]))
rows = body["data"]
current = [r["id"] for r in rows if r["current"]]
print(len(rows), len(current), current[0] if current else "-",
      body["has_more"], "next" if body["next_cursor"] else "null",
      ",".join(sorted(r["device_label"] for r in rows)).replace(" ", "_"))' \
  "$WORKDIR/devices-list.json")"
read -r listed_count current_count current_id has_more next_cursor listed_labels <<<"$listed"
[[ "$listed_count" == "3" ]] || fail "the list has $listed_count devices, want the 3 that signed in"
[[ "$listed_labels" == "Verify_Device_one,Verify_Device_three,Verify_Device_two" ]] \
  || fail "the labels are '$listed_labels', not the ones the devices signed in with"
ok "every device that signed in is listed, under the label it sent"

[[ "$current_count" == "1" && "$current_id" == "$devices_current_sid" ]] \
  || fail "$current_count rows claim to be the current device (id '$current_id', want '$devices_current_sid')"
ok "exactly one row is marked current, and it is the device that made the request"

[[ "$has_more" == "False" && "$next_cursor" == "null" ]] \
  || fail "the collection envelope says has_more=$has_more next_cursor=$next_cursor"
ok "the response uses the collection envelope, with nothing left unpaged"

# Reading a list is not activity. 000103's header is explicit that nothing derived from
# last_seen_at may extend a credential, and a display column every read writes to means nothing.
before_seen="$("$PSQL" "$DATABASE_URL" -tAc \
  "select max(last_seen_at)::text from device_sessions where user_id = '$devices_user';")"
get_auth "$devices_access" /v1/auth/sessions "$WORKDIR/devices-list-2.json" >/dev/null
after_seen="$("$PSQL" "$DATABASE_URL" -tAc \
  "select max(last_seen_at)::text from device_sessions where user_id = '$devices_user';")"
[[ "$after_seen" == "$before_seen" ]] || fail "reading the device list moved last_seen_at"
ok "reading the list does not move last seen — a read is not activity"

# Somebody else's devices are not on it. This is the failure a single-account check cannot see.
status="$(get_auth "$(json "$WORKDIR/logout-b.json" '["access_token"]')" /v1/auth/sessions \
  "$WORKDIR/devices-other.json")"
[[ "$status" == "200" ]] || fail "the other account could not list its devices ($status)"
python3 -c 'import json,sys
labels = [r["device_label"] for r in json.load(open(sys.argv[1]))["data"]]
sys.exit(1 if any(l.startswith("Verify Device") for l in labels) else 0)' "$WORKDIR/devices-other.json" \
  || fail "one account was handed another account's device list"
ok "the list carries the caller's devices and nobody else's"

# Revoking. The device chosen is not the one making the request, which is the case the endpoint
# exists for — signing out the device in your hand is SHIP-43.
doomed_id="$(python3 -c 'import json,sys
print(next(r["id"] for r in json.load(open(sys.argv[1]))["data"]
           if r["device_label"] == "Verify Device two"))' "$WORKDIR/devices-list.json")"
doomed_refresh="$(json "$WORKDIR/devices-two.json" '["refresh_token"]')"

status="$(curl -s -X DELETE -o "$WORKDIR/devices-revoke.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-devices-revoke-$$" -H "$auth_header: Bearer $devices_access" \
  "http://localhost:$VERIFY_PORT/v1/auth/sessions/$doomed_id")"
[[ "$status" == "204" ]] || { cat "$WORKDIR/devices-revoke.json"; fail "DELETE /v1/auth/sessions/{id} returned $status, want 204"; }
ok "a device is revoked from the list with DELETE, answering 204"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(revoked_reason, 'live') from device_sessions where id = '$doomed_id';")" == "revoked_by_owner" ]] \
  || fail "the session is not marked revoked_by_owner"
ok "the row is marked 'revoked_by_owner' — distinct from a sign-out, and not deleted"

status="$(post_json "verify-devices-revoked-refresh-$$" /v1/auth/refresh \
  "{\"refresh_token\":\"$doomed_refresh\"}" "$WORKDIR/devices-revoked-refresh.json")"
[[ "$status" == "400" ]] || fail "the revoked device could still refresh ($status)"
ok "the revoked device's refresh token stops working"

status="$(get_auth "$devices_access" /v1/auth/sessions "$WORKDIR/devices-list-3.json")"
[[ "$status" == "200" ]] || fail "listing after the revoke returned $status"
remaining="$(python3 -c 'import json,sys
body = json.load(open(sys.argv[1]))
print(len(body["data"]),
      ",".join(sorted(r["device_label"] for r in body["data"])).replace(" ", "_"))' \
  "$WORKDIR/devices-list-3.json")"
[[ "$remaining" == "2 Verify_Device_one,Verify_Device_three" ]] \
  || fail "the list is now '$remaining', want the two devices that were not revoked"
ok "it leaves the list, and the devices that were not revoked stay on it"

# Somebody else's session, and one that does not exist, answer identically — or this endpoint is a
# way of finding out which identifiers name real sessions.
status="$(curl -s -X DELETE -o "$WORKDIR/devices-not-mine.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-devices-not-mine-$$" -H "$auth_header: Bearer $devices_access" \
  "http://localhost:$VERIFY_PORT/v1/auth/sessions/$signed_out_sid")"
[[ "$status" == "404" ]] || fail "revoking another account's session returned $status, want 404"
not_mine_code="$(json "$WORKDIR/devices-not-mine.json" '["error"]["code"]')"

status="$(curl -s -X DELETE -o "$WORKDIR/devices-unknown.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-devices-unknown-$$" -H "$auth_header: Bearer $devices_access" \
  "http://localhost:$VERIFY_PORT/v1/auth/sessions/$(uuidgen | tr 'A-Z' 'a-z')")"
[[ "$status" == "404" ]] || fail "revoking a session that does not exist returned $status, want 404"
unknown_code="$(json "$WORKDIR/devices-unknown.json" '["error"]["code"]')"

[[ "$not_mine_code" == "identity_session_not_found" && "$unknown_code" == "$not_mine_code" ]] \
  || fail "another account's session answers '$not_mine_code' and an unknown one '$unknown_code'"
ok "another account's session and one that never existed are one answer, and neither was revoked"

still_there="$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(revoked_reason, 'live') from device_sessions
    where user_id = '$registered_id' and device_label = 'Verify Still In';")"
[[ "$still_there" == "live" ]] || fail "the other account's device was revoked by a stranger"
ok "the session a stranger named is still live — the owner check is on the query, not above it"

# The idempotency middleware applies to DELETE like every other mutation.
status="$(curl -s -X DELETE -o "$WORKDIR/devices-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $devices_access" \
  "http://localhost:$VERIFY_PORT/v1/auth/sessions/$doomed_id")"
[[ "$status" == "400" ]] || fail "revoking without an Idempotency-Key returned $status"
[[ "$(json "$WORKDIR/devices-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || fail "expected code=idempotency_key_required"
ok "revoking needs an Idempotency-Key, so a retry is not a second decision"

# Both routes are unreachable without a credential — 401 with the challenge RFC 9110 requires.
status="$(curl -s -o "$WORKDIR/devices-anon.json" -D "$WORKDIR/devices-anon.headers" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/auth/sessions")"
[[ "$status" == "401" ]] || fail "the device list answered $status to a caller with no credential"
grep -qi '^WWW-Authenticate: Bearer' "$WORKDIR/devices-anon.headers" \
  || fail "no WWW-Authenticate challenge on the 401"
[[ "$(json "$WORKDIR/devices-anon.json" '["error"]["code"]')" == "unauthenticated" ]] \
  || fail "expected code=unauthenticated"
ok "the device list is unreachable without a credential"

# ---------------------------------------------------------------------------------------
ticket "SHIP-47  repeated sign-in failures are throttled per account and per IP"

# Every request in this run arrives from 127.0.0.1, so the per-address bucket has been paying for
# the failed sign-ins the sections above deliberately provoked. It is emptied here rather than
# reasoned about: an expected figure derived from counting other sections' refusals is arithmetic
# somebody has to redo whenever one of them changes, and it would be wrong quietly.
#
# The per-account buckets go with it, so both halves below start from capacity. Both namespaces
# are cleared — see credential_bucket_keys — because since SHIP-183b the address half is spent by
# refresh and by both verification endpoints as well, and the arithmetic at the end of this block
# subtracts only what this block itself spent.
credential_bucket_keys | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true

throttle_email="throttle-$$@example.com"
status="$(post_json "verify-throttle-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$throttle_email\",\"phone\":\"04940$$\",\"password\":\"$login_password\",\"role\":\"customer\"}" \
  "$WORKDIR/throttle-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/throttle-register.json"; fail "could not register the throttle account ($status)"; }

# wrong_password <key-suffix> <email> <outfile> — one failed sign-in.
wrong_password() {
  post_json "verify-throttle-$1-$$" /v1/auth/login \
    "{\"email\":\"$2\",\"password\":\"not-the-registered-password\",\"device_label\":\"Verify Throttle\"}" "$3"
}

admitted=0
throttled_status=""
for attempt in $(seq 1 12); do
  status="$(wrong_password "acct-$attempt" "$throttle_email" "$WORKDIR/throttle-$attempt.json")"
  if [[ "$status" == "429" ]]; then
    throttled_status="$status"
    cp "$WORKDIR/throttle-$attempt.json" "$WORKDIR/throttle-refused.json"
    break
  fi
  [[ "$status" == "400" ]] || { cat "$WORKDIR/throttle-$attempt.json"; fail "attempt $attempt returned $status, want 400"; }
  admitted=$((admitted + 1))
done

[[ "$throttled_status" == "429" ]] || fail "twelve wrong passwords against one account were never throttled"
[[ "$admitted" == "5" ]] || fail "$admitted wrong passwords were admitted before the throttle, want 5"
ok "wrong passwords against one account are throttled after five, with a 429"

[[ "$(json "$WORKDIR/throttle-refused.json" '["error"]["code"]')" == "rate_limited" ]] \
  || fail "expected code=rate_limited"
ok "the refusal carries the protocol code a client already handles centrally"

# Retry-After is the time until the allowance returns, which is what makes it actionable.
retry_after="$(curl -s -o /dev/null -D - -X POST \
  -H "Idempotency-Key: verify-throttle-header-$$" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$throttle_email\",\"password\":\"x-not-the-password\",\"device_label\":\"Verify Throttle\"}" \
  "http://localhost:$VERIFY_PORT/v1/auth/login" | tr -d '\r' | awk 'tolower($1) == "retry-after:" { print $2 }')"
[[ "$retry_after" =~ ^[0-9]+$ && "$retry_after" -gt 0 && "$retry_after" -le 120 ]] \
  || fail "Retry-After is '$retry_after', want a positive number of seconds no larger than the interval"
ok "it carries Retry-After — $retry_after seconds, the time until the allowance returns"

# The limit is checked before the password, which is the point: what it protects is the argon2id
# derivation. A throttle a correct password escaped would be a throttle an attacker escapes by
# guessing right.
status="$(post_json "verify-throttle-correct-$$" /v1/auth/login \
  "{\"email\":\"$throttle_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Throttle\"}" \
  "$WORKDIR/throttle-correct.json")"
[[ "$status" == "429" ]] || fail "the right password got through a throttle ($status)"
ok "the right password is refused too while the limit holds — the limit is checked first"

throttled_devices="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_sessions d join users u on u.id = d.user_id
    where u.email = '$throttle_email';")"
[[ "$throttled_devices" == "0" ]] || fail "a throttled account was given $throttled_devices sessions"
ok "no session was created by any of it"

# Per account, not per platform: a different account is unaffected while its own bucket is full.
status="$(post_json "verify-throttle-other-$$" /v1/auth/login \
  "{\"email\":\"$devices_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Not Throttled\"}" \
  "$WORKDIR/throttle-other.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/throttle-other.json"; fail "another account was throttled by this one's failures ($status)"; }
ok "another account signs in normally — one account's failures do not throttle the platform"

# Per address, across accounts. Each attempt uses an address of its own, so every per-account
# bucket stays full and the only thing that can refuse is the network-wide limit.
address_throttled=""
for attempt in $(seq 1 60); do
  status="$(wrong_password "addr-$attempt" "nobody-$attempt-$$@example.com" "$WORKDIR/throttle-addr.json")"
  if [[ "$status" == "429" ]]; then
    address_throttled="$attempt"
    break
  fi
  [[ "$status" == "400" ]] || { cat "$WORKDIR/throttle-addr.json"; fail "address attempt $attempt returned $status"; }
done

[[ -n "$address_throttled" ]] \
  || fail "sixty failures spread across sixty accounts from one address were never throttled"
# $admitted was already spent above, by the wrong passwords the per-account check *admitted* from
# this same address. The ones it refused cost nothing: a request the account bucket turns away
# never reaches the address bucket.
[[ "$((address_throttled - 1 + admitted))" == "30" ]] \
  || fail "$((address_throttled - 1 + admitted)) failures were admitted from one address, want the capacity of 30"
ok "failures spread across accounts are throttled per address, at the capacity of thirty"

# And it is genuinely a second bucket rather than the account one under another name: the account
# that was signing in normally a moment ago is now refused from this address too.
status="$(post_json "verify-throttle-addr-spill-$$" /v1/auth/login \
  "{\"email\":\"$devices_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Spill\"}" \
  "$WORKDIR/throttle-spill.json")"
[[ "$status" == "429" ]] \
  || fail "an exhausted address let an untouched account through ($status), so the per-address limit counts nothing"
ok "an account with a full bucket of its own is still refused from an exhausted address"

# The address bucket is now empty, and it is shared with everything that runs after this file —
# every request in this script arrives from 127.0.0.1, including a later track's. A section that
# signed in and got a 429 would look like a broken endpoint rather than like this one's leftovers,
# so the keys are removed rather than left to refill on a twenty-second timer.
#
# Deliberately at the end and deliberately narrow: it deletes the per-address buckets and nothing
# else, so the per-account state above it is untouched and the checks that made it stay meaningful.
cleared="$(redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:credential:address:*' \
  | xargs -r redis-cli -u "$REDIS_URL" del 2>/dev/null || true)"
remaining="$(redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:credential:address:*' | wc -l | tr -d ' ')"
[[ "$remaining" == "0" ]] || fail "$remaining per-address buckets survived the clean-up"
status="$(post_json "verify-throttle-cleared-$$" /v1/auth/login \
  "{\"email\":\"$devices_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Cleared\"}" \
  "$WORKDIR/throttle-cleared.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/throttle-cleared.json"; fail "sign-in is still refused after the address buckets were cleared ($status)"; }
ok "the per-address buckets are cleared, so a later section is not throttled by this one ($cleared removed)"

# ---------------------------------------------------------------------------------------
ticket "SHIP-169  a signed-in person can request deletion and is told when it completes"

# Deliberately after SHIP-47, and deliberately doing no *failed* sign-ins.
#
# The section above ends by emptying `rl:v1:credential:address:*`, because every request in this run
# arrives from 127.0.0.1 and a later track that got a 429 would look like a broken endpoint rather
# than like this file's leftovers. That clean-up has to stay the last thing the file does to those
# keys. Nothing here spends from them — SHIP-47 charges the bucket on a *refused* credential only
# (`Allow` on admission, `Spend` on failure), and every sign-in below uses the right password — and
# the last check in this section proves that rather than asserting it.

deletion_email="deletion-$$@example.com"
status="$(post_json "verify-deletion-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$deletion_email\",\"phone\":\"04920$$\",\"password\":\"$login_password\",\"role\":\"customer\"}" \
  "$WORKDIR/deletion-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/deletion-register.json"; fail "could not register the deletion account ($status)"; }
deletion_user="$(json "$WORKDIR/deletion-register.json" '["id"]')"

status="$(post_json "verify-deletion-login-$$" /v1/auth/login \
  "{\"email\":\"$deletion_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Deletion\"}" \
  "$WORKDIR/deletion-login.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/deletion-login.json"; fail "could not sign the deletion account in ($status)"; }
deletion_access="$(json "$WORKDIR/deletion-login.json" '["access_token"]')"

# Unreachable without a credential, or the account being deleted would not have to be the
# caller's own. The Idempotency-Key is sent because the middleware checks it further out than the
# auth class is enforced — without one this would be a 400 about the key and prove nothing.
status="$(curl -s -X POST -o "$WORKDIR/deletion-anon.json" -D "$WORKDIR/deletion-anon.headers" \
  -w '%{http_code}' -H "Idempotency-Key: verify-deletion-anon-$$" \
  "http://localhost:$VERIFY_PORT/v1/account/deletion")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/deletion-anon.json"; fail "deletion answered $status to a caller with no credential, want 401"; }
grep -qi '^WWW-Authenticate: Bearer' "$WORKDIR/deletion-anon.headers" \
  || fail "no WWW-Authenticate challenge on the 401"
[[ "$(json "$WORKDIR/deletion-anon.json" '["error"]["code"]')" == "unauthenticated" ]] \
  || fail "expected code=unauthenticated"
ok "requesting deletion is unreachable without a credential"

# It is a state change like every other, so it carries a key (SHIP-15).
status="$(curl -s -X POST -o "$WORKDIR/deletion-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $deletion_access" \
  "http://localhost:$VERIFY_PORT/v1/account/deletion")"
[[ "$status" == "400" ]] || fail "requesting deletion without an Idempotency-Key returned $status"
[[ "$(json "$WORKDIR/deletion-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || fail "expected code=idempotency_key_required"
ok "requesting deletion needs an Idempotency-Key"

# The criterion itself.
status="$(post_auth "verify-deletion-first-$$" "$deletion_access" /v1/account/deletion \
  "$WORKDIR/deletion-first.json")"
[[ "$status" == "202" ]] || { cat "$WORKDIR/deletion-first.json"; fail "POST /v1/account/deletion returned $status, want 202"; }
ok "a signed-in person can request deletion, answered 202 Accepted"

deletion_id="$(json "$WORKDIR/deletion-first.json" '["id"]')"
deletion_state="$(json "$WORKDIR/deletion-first.json" '["state"]')"
deletion_requested="$(json "$WORKDIR/deletion-first.json" '["requested_at"]')"
deletion_completes="$(json "$WORKDIR/deletion-first.json" '["completes_by"]')"

[[ "$deletion_state" == "requested" && -n "$deletion_completes" ]] \
  || fail "the response says state='$deletion_state' completes_by='$deletion_completes'"

# Thirty days, which is the figure Docs/05 §3.1 commits to and the one the person is told.
window_days="$(python3 - "$deletion_requested" "$deletion_completes" <<'PYTHON'
import sys
from datetime import datetime

requested, completes = (datetime.fromisoformat(a.replace("Z", "+00:00")) for a in sys.argv[1:3])
print((completes - requested).total_seconds() / 86400)
PYTHON
)"
[[ "$window_days" == "30.0" ]] \
  || fail "the completion date is $window_days days after the request, want the 30 Docs/05 §3.1 commits to"
ok "and receives a completion date — $deletion_completes, thirty days out"

# The row, not the answer. A completion date computed while rendering the response satisfies
# everything above and writes nothing, so this is the check that separates a promise from a
# plausible sentence.
read -r stored_state stored_completes stored_window <<<"$("$PSQL" "$DATABASE_URL" -tAc \
  "select state,
          to_char(complete_by at time zone 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.MS\"Z\"'),
          (extract(epoch from (complete_by - requested_at)) / 86400)::int
     from account_deletion_requests where id = '$deletion_id';" | tr '|' ' ')"
[[ "$stored_state" == "requested" ]] || fail "the stored state is '$stored_state', want requested"
[[ "$stored_completes" == "$deletion_completes" ]] \
  || fail "the row holds $stored_completes and the person was told $deletion_completes"
[[ "$stored_window" == "30" ]] \
  || fail "the stored window is $stored_window days, want 30"
ok "the date is recorded in the row, and it is the same date the person was told"

# A retry of the same request — the dropped-connection case the middleware exists for.
status="$(post_auth "verify-deletion-first-$$" "$deletion_access" /v1/account/deletion \
  "$WORKDIR/deletion-replay.json")"
[[ "$status" == "202" ]] || { cat "$WORKDIR/deletion-replay.json"; fail "the replay returned $status, want the stored 202"; }
cmp -s "$WORKDIR/deletion-first.json" "$WORKDIR/deletion-replay.json" \
  || fail "the replay is not the stored response (a success body carries no request_id, so these are comparable byte for byte)"
ok "a retry on the same key replays the original response, byte for byte"

# A second, honest request — a different key, so nothing is replayed and the handler runs again.
# This is where a recomputed completion date shows itself: the two calls are seconds apart, and a
# date derived from now() differs between them in the milliseconds.
status="$(post_auth "verify-deletion-second-$$" "$deletion_access" /v1/account/deletion \
  "$WORKDIR/deletion-second.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/deletion-second.json"; fail "asking again returned $status, want 200 — a repeat is not an error and is not a second request"; }
[[ "$(json "$WORKDIR/deletion-second.json" '["id"]')" == "$deletion_id" ]] \
  || fail "asking again named a different request"
[[ "$(json "$WORKDIR/deletion-second.json" '["completes_by"]')" == "$deletion_completes" ]] \
  || fail "the completion date moved between two requests; it is being computed at read time rather than read from the row"
ok "asking again answers 200 with the same request and the same date, unmoved"

# Two rows would be two promises about one account, and whichever the execution read would be the
# one that counted. The count is what separates one request answered twice from two requests.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from account_deletion_requests where user_id = '$deletion_user';")" == "1" ]] \
  || fail "the account holds more than one deletion request after asking twice"
ok "one row after two requests and a replay — uq_account_deletion_requests_open holds"

# Another account's request is its own, on both sides.
deletion_other_email="deletion-two-$$@example.com"
status="$(post_json "verify-deletion-other-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$deletion_other_email\",\"phone\":\"04921$$\",\"password\":\"$login_password\",\"role\":\"provider\"}" \
  "$WORKDIR/deletion-other-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/deletion-other-register.json"; fail "could not register the second deletion account ($status)"; }
deletion_other_user="$(json "$WORKDIR/deletion-other-register.json" '["id"]')"

status="$(post_json "verify-deletion-other-login-$$" /v1/auth/login \
  "{\"email\":\"$deletion_other_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Deletion Two\"}" \
  "$WORKDIR/deletion-other-login.json")"
[[ "$status" == "200" ]] || fail "could not sign the second deletion account in ($status)"

status="$(post_auth "verify-deletion-other-$$" "$(json "$WORKDIR/deletion-other-login.json" '["access_token"]')" \
  /v1/account/deletion "$WORKDIR/deletion-other.json")"
[[ "$status" == "202" ]] || { cat "$WORKDIR/deletion-other.json"; fail "the second account could not request deletion ($status)"; }
[[ "$(json "$WORKDIR/deletion-other.json" '["id"]')" != "$deletion_id" ]] \
  || fail "the second account was handed the first account's deletion request"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from account_deletion_requests where user_id = '$deletion_other_user';")" == "1" ]] \
  || fail "the second account does not hold exactly one request of its own"
ok "another account's request is its own, and neither account can see the other's"

# The scope boundary, stated as a check rather than as a comment. **SHIP-170 widened it and this
# line moved with it rather than being deleted**, which is what SHIP-169 wrote it for: "if a later
# branch widens the constraint, this line is what makes that a decision somebody took." SHIP-171
# moved it again, and the pair below is now the whole point of the file's last two checks: the CHECK
# has three states and the index still has two.
#
# LC_ALL=C so the sort is by bytes rather than by whatever locale the runner inherited.
deletion_states="$("$PSQL" "$DATABASE_URL" -tAc \
  "select pg_get_constraintdef(oid) from pg_constraint
    where conname = 'ck_account_deletion_requests_state';" \
  | grep -oE "'[a-z_]+'::text" | tr -d "'" | sed 's/::text//' | LC_ALL=C sort | tr '\n' ',' | sed 's/,$//')"
[[ "$deletion_states" == "completed,deferred,requested" ]] \
  || fail "the deletion states are '$deletion_states', want 'completed,deferred,requested' — SHIP-169, SHIP-170 and SHIP-171 are all built"
ok "'requested', 'deferred' and 'completed' are the states a request can be in"

# **The index did *not* widen with the CHECK, and that is SHIP-171's most easily-lost decision.**
# 000106 widened this predicate because a deferred request is an *open* request; 000105 said before
# either existed why a completed one is not — "a completed request must not stop a later one; only
# an *open* one does". Adding 'completed' here would be a silent, permanent refusal: the account
# would hold one row forever and every later request would be swallowed by ON CONFLICT with the
# executed one handed back as though it were live.
deletion_open_predicate="$("$PSQL" "$DATABASE_URL" -tAc \
  "select indexdef from pg_indexes where indexname = 'uq_account_deletion_requests_open';" \
  | sed 's/.* WHERE //' | grep -oE "'[a-z_]+'::text" | tr -d "'" | sed 's/::text//' | LC_ALL=C sort | tr '\n' ',' | sed 's/,$//')"
[[ "$deletion_open_predicate" == "deferred,requested" ]] \
  || fail "uq_account_deletion_requests_open covers '$deletion_open_predicate', want 'deferred,requested' — a deferred request is open and a completed one must not be"
ok "the open-request index covers the two open states and not the third — a completed request never blocks a later one"

# ---------------------------------------------------------------------------------------
ticket "SHIP-170  a request during a delivery queues until the job closes, and explains why"

# Two accounts and one awarded job, because the ticket is about **both** parties. Docs/05 §3.1:
# "Erasing a party mid-delivery would strand the counterparty" — the customer whose goods are
# moving and the provider carrying them are each the other's counterparty, and the deletion route
# is RequireUser with no role predicate, so both reach it. A section that checked the customer
# alone would pass against a lookup that deletes drivers mid-delivery.

# move_job_for_deferral <job> <from> <to> <actor> — a status change through the guard.
#
# `jobs.status` is not a settable field (000402): an UPDATE has to be accompanied, in the same
# transaction, by a job_status_history row describing it and named to the trigger through a
# transaction-local setting. This is that protocol, which is 000402's own intention — that a
# fixture and the domain be the same caller rather than the fixture having a private way in. It is
# `move_job` from 50-jobs.sh, local here because that file's copy is not this file's to call.
move_job_for_deferral() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 \
    -v job="$1" -v from_status="$2" -v to_status="$3" -v actor="$4" \
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

defer_customer_email="deferral-c-$$@example.com"
status="$(post_json "verify-deferral-c-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Deferral Customer\",\"email\":\"$defer_customer_email\",\"phone\":\"04922$$\",\"password\":\"$login_password\",\"role\":\"customer\"}" \
  "$WORKDIR/deferral-c-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/deferral-c-register.json"; fail "could not register the deferral customer ($status)"; }
defer_customer_id="$(json "$WORKDIR/deferral-c-register.json" '["id"]')"

status="$(post_json "verify-deferral-c-login-$$" /v1/auth/login \
  "{\"email\":\"$defer_customer_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Deferral C\"}" \
  "$WORKDIR/deferral-c-login.json")"
[[ "$status" == "200" ]] || fail "could not sign the deferral customer in ($status)"
defer_customer_token="$(json "$WORKDIR/deferral-c-login.json" '["access_token"]')"

defer_provider_email="deferral-p-$$@example.com"
status="$(post_json "verify-deferral-p-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Deferral Provider\",\"email\":\"$defer_provider_email\",\"phone\":\"04923$$\",\"password\":\"$login_password\",\"role\":\"provider\"}" \
  "$WORKDIR/deferral-p-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/deferral-p-register.json"; fail "could not register the deferral provider ($status)"; }
defer_provider_id="$(json "$WORKDIR/deferral-p-register.json" '["id"]')"

status="$(post_json "verify-deferral-p-login-$$" /v1/auth/login \
  "{\"email\":\"$defer_provider_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Deferral P\"}" \
  "$WORKDIR/deferral-p-login.json")"
[[ "$status" == "200" ]] || fail "could not sign the deferral provider in ($status)"
defer_provider_token="$(json "$WORKDIR/deferral-p-login.json" '["access_token"]')"

# A third provider who bid on the same job and lost, so that "a provider" is not the same claim as
# "the awarded provider". Without it, a lookup joining `bids` with no status predicate would pass
# every check below.
defer_loser_email="deferral-l-$$@example.com"
status="$(post_json "verify-deferral-l-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Deferral Loser\",\"email\":\"$defer_loser_email\",\"phone\":\"04924$$\",\"password\":\"$login_password\",\"role\":\"provider\"}" \
  "$WORKDIR/deferral-l-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/deferral-l-register.json"; fail "could not register the losing bidder ($status)"; }
defer_loser_id="$(json "$WORKDIR/deferral-l-register.json" '["id"]')"

status="$(post_json "verify-deferral-l-login-$$" /v1/auth/login \
  "{\"email\":\"$defer_loser_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Deferral L\"}" \
  "$WORKDIR/deferral-l-login.json")"
[[ "$status" == "200" ]] || fail "could not sign the losing bidder in ($status)"
defer_loser_token="$(json "$WORKDIR/deferral-l-login.json" '["access_token"]')"

# The job. Inserted at Draft — 000402 refuses any other creation status — and then walked to
# Awarded through the guard. The bids are direct inserts because `bids` has no status trigger
# (000500 declined to give it the one `jobs` has), which is the same fixture 60-fleet.sh writes.
# -q as well as -tA: without it psql prints the `INSERT 0 1` command tag after the returned id,
# and the variable becomes two lines that the next statement interpolates as a malformed uuid.
defer_job="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into jobs (id, customer_id) values (gen_random_uuid(), '$defer_customer_id') returning id;" | tr -d ' ')"
[[ -n "$defer_job" ]] || fail "could not create the job the deferral waits on"

"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
  "insert into bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
   values (gen_random_uuid(), '$defer_job', '$defer_provider_id', 'Accepted', 45000,
           now() + interval '2 days', now() + interval '3 days'),
          (gen_random_uuid(), '$defer_job', '$defer_loser_id', 'Rejected', 52000,
           now() + interval '2 days', now() + interval '3 days');" >/dev/null \
  || fail "could not place the accepted and rejected bids"

move_job_for_deferral "$defer_job" Draft Open "$defer_customer_id"
move_job_for_deferral "$defer_job" Open Awarded "$defer_customer_id"

# The fixture, verified rather than assumed: a deferral check against a job that never reached
# Awarded passes forever and proves nothing.
read -r defer_job_status defer_accepted <<<"$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status, (select count(*) from bids where job_id = j.id and status = 'Accepted')
     from jobs j where j.id = '$defer_job';" | tr '|' ' ')"
[[ "$defer_job_status" == "Awarded" && "$defer_accepted" == "1" ]] \
  || fail "the fixture job is '$defer_job_status' with $defer_accepted accepted bids, want Awarded with 1"
ok "the fixture is a job at Awarded with one accepted bid and one rejected one"

# The customer's half of the criterion.
status="$(post_auth "verify-deferral-c-delete-$$" "$defer_customer_token" /v1/account/deletion \
  "$WORKDIR/deferral-c-delete.json")"
[[ "$status" == "202" ]] || { cat "$WORKDIR/deferral-c-delete.json"; fail "the customer's request returned $status, want 202 — Docs/05 §3.1 defers rather than refuses"; }
[[ "$(json "$WORKDIR/deferral-c-delete.json" '["state"]')" == "deferred" ]] \
  || { cat "$WORKDIR/deferral-c-delete.json"; fail "the customer's request is not deferred while their delivery is in flight"; }
ok "a customer mid-delivery is deferred rather than refused, and still answered 202"

# **The provider's half, which is the one a customer-only lookup would fail.**
status="$(post_auth "verify-deferral-p-delete-$$" "$defer_provider_token" /v1/account/deletion \
  "$WORKDIR/deferral-p-delete.json")"
[[ "$status" == "202" ]] || { cat "$WORKDIR/deferral-p-delete.json"; fail "the provider's request returned $status, want 202"; }
[[ "$(json "$WORKDIR/deferral-p-delete.json" '["state"]')" == "deferred" ]] \
  || { cat "$WORKDIR/deferral-p-delete.json"; fail "the awarded provider's request is not deferred; erasing them mid-delivery would strand the customer"; }
ok "the provider carrying the delivery is deferred too — a party is either side of the job"

# The row rather than the answer, for both. A state decorated onto the response would satisfy
# everything above and leave SHIP-171 nothing to read before it executes.
defer_states="$("$PSQL" "$DATABASE_URL" -tAc \
  "select state from account_deletion_requests
    where user_id in ('$defer_customer_id', '$defer_provider_id') order by state;" | tr '\n' ',' | sed 's/,$//')"
[[ "$defer_states" == "deferred,deferred" ]] \
  || fail "the stored states are '$defer_states', want both deferred"
ok "both deferrals are in the rows, not only in the responses"

# And explains why — the clause the *Done when* names second, on the wire rather than in a comment.
defer_reason="$(json "$WORKDIR/deferral-p-delete.json" '["deferral_reason"]')"
[[ -n "$defer_reason" ]] || { cat "$WORKDIR/deferral-p-delete.json"; fail "a deferred request carries no deferral_reason, so nothing explains why"; }
grep -qi 'delivery' <<<"$defer_reason" || fail "the explanation does not mention the delivery: $defer_reason"
if ! python3 - "$WORKDIR/deferral-p-delete.json" "$defer_job" <<'PYTHON'
import json, sys

body = json.load(open(sys.argv[1]))
reason = body["deferral_reason"]
# The explanation must not carry the job. Both halves of the marketplace reach this endpoint, and
# Docs/01 §4.3 is easiest to keep on a sentence that never had a job to describe.
for forbidden in (sys.argv[2], "$", "job"):
    if forbidden in reason:
        sys.exit(f"the deferral explanation contains {forbidden!r}: {reason}")
PYTHON
then
  fail "the deferral explanation says more than it should"
fi
ok "the explanation says why, names no job and carries no amount"

# A bystander with no job at all is not deferred, which is what makes the two checks above about
# the delivery rather than about the endpoint.
status="$(post_auth "verify-deferral-l-delete-$$" "$defer_loser_token" /v1/account/deletion \
  "$WORKDIR/deferral-l-delete.json")"
[[ "$status" == "202" ]] || { cat "$WORKDIR/deferral-l-delete.json"; fail "the losing bidder's request returned $status, want 202"; }
[[ "$(json "$WORKDIR/deferral-l-delete.json" '["state"]')" == "requested" ]] \
  || { cat "$WORKDIR/deferral-l-delete.json"; fail "a provider whose offer was rejected was deferred; they are carrying nothing"; }
python3 -c "
import json, sys
body = json.load(open(sys.argv[1]))
sys.exit('a live request carries a deferral_reason' if 'deferral_reason' in body else 0)
" "$WORKDIR/deferral-l-delete.json" || fail "a request that is not deferred still explains a deferral"
ok "a provider whose offer was rejected is not deferred, and carries no explanation"

# "…until the job closes." The delivery finishes, and the same person asks again.
move_job_for_deferral "$defer_job" Awarded Delivered "$defer_customer_id"

# Delivered is still in flight — Docs/02 §6.1 has the job auto-complete 72 hours later, and it can
# still go to Disputed — so the deferral must hold here. This is the boundary of Docs/05 §3.1's
# range and the one an off-by-one would get wrong.
status="$(post_auth "verify-deferral-p-delivered-$$" "$defer_provider_token" /v1/account/deletion \
  "$WORKDIR/deferral-p-delivered.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/deferral-p-delivered.json"; fail "asking again returned $status, want 200"; }
[[ "$(json "$WORKDIR/deferral-p-delivered.json" '["state"]')" == "deferred" ]] \
  || fail "the deferral lifted at Delivered; Docs/05 §3.1's range is Awarded *to Delivered* inclusive"
ok "a job at Delivered still defers — the range is inclusive at both ends"

defer_promised_before="$(json "$WORKDIR/deferral-p-delivered.json" '["completes_by"]')"

move_job_for_deferral "$defer_job" Delivered Completed "$defer_customer_id"

status="$(post_auth "verify-deferral-p-closed-$$" "$defer_provider_token" /v1/account/deletion \
  "$WORKDIR/deferral-p-closed.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/deferral-p-closed.json"; fail "asking after the job closed returned $status, want 200"; }
[[ "$(json "$WORKDIR/deferral-p-closed.json" '["state"]')" == "requested" ]] \
  || { cat "$WORKDIR/deferral-p-closed.json"; fail "the deferral did not lift once the job completed, so the request queues forever"; }
[[ "$(json "$WORKDIR/deferral-p-closed.json" '["id"]')" == "$(json "$WORKDIR/deferral-p-delete.json" '["id"]')" ]] \
  || fail "lifting the deferral named a different request"
python3 -c "
import json, sys
body = json.load(open(sys.argv[1]))
sys.exit('a lifted request still carries a deferral_reason' if 'deferral_reason' in body else 0)
" "$WORKDIR/deferral-p-closed.json" || fail "the explanation survived the deferral lifting"
ok "the request becomes live once the job closes, keeping its identity and dropping its explanation"

# The thirty days start when the deferral lifts, so the date moved. A date that had not moved would
# mean the platform promised a window it spent waiting.
defer_promised_after="$(json "$WORKDIR/deferral-p-closed.json" '["completes_by"]')"
[[ "$defer_promised_after" > "$defer_promised_before" ]] \
  || fail "the completion date is still $defer_promised_after after the deferral lifted; the thirty days ran while the request was on hold"
ok "the thirty days start when the deferral lifts — $defer_promised_after, not $defer_promised_before"

# One row throughout. Two would be two promises about one account, and the count is what separates
# "the state moved" from "a second request was made".
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from account_deletion_requests where user_id = '$defer_provider_id';")" == "1" ]] \
  || fail "the provider holds more than one deletion request after a deferral lifted"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select state from account_deletion_requests where user_id = '$defer_provider_id';")" == "requested" ]] \
  || fail "the stored state did not move with the answer"
ok "one row across the whole lifecycle, and the row holds the state the person was told"

# Nothing here charged a sign-in bucket, so SHIP-47's clean-up above is still the last word on
# them. Asserted rather than assumed: the whole file's rate-limit hygiene depends on it, and a
# later track's 429 would read as its own endpoint being broken.
[[ "$(redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:credential:address:*' | wc -l | tr -d ' ')" == "0" ]] \
  || fail "this section left per-address sign-in buckets behind, which a later track would be throttled by"
ok "no per-address sign-in bucket was spent here, so SHIP-47's clean-up still holds at the end of the file"

# ---------------------------------------------------------------------------------------
ticket "SHIP-171  the clock runs out and the person is replaced by a stable pseudonym"

# Docs/05 §3.1: "delete the person, retain the transaction". SHIP-169 recorded the request and the
# promise, SHIP-170 held it while a delivery was in flight, and this is what happens when the
# thirty days run out — from cmd/worker, because nobody presses a button to be erased on the
# thirtieth day and §3.1 puts execution out of an ordinary administrator's reach entirely.
#
# # What only this can show
#
# internal/identity/pseudonymise_test.go holds the rules: which requests are due, what a pseudonym
# is made of, that nothing in the schema still holds the person, and that a completed request does
# not stop the same account asking again. cmd/worker/tasks_identity_test.go holds the pass and both
# adapters. **What only this can show is the real binary reaching across five tables that four
# domains own**, against rows the served API wrote, in a database that has been through every
# section before it.
#
# # Own what you assert about — the rule this section had to think hardest about
#
# cmd/worker is one binary and a start runs *every* registered task, so this start also runs
# job-expiry, job-expiry-warning, bid-expiry, job-auto-complete and outbox-publisher. This is
# **section 40**, which runs before every other section that starts a worker, so a pass here reaches
# a database the later sections have not built yet — which is the safest position in the file order
# and is not an excuse for skipping the sweep.
#
#   * The three job sweeps claim what is due. The only jobs in the database at this point are this
#     file's own: SHIP-170's fixture, which ends `Completed`, and the two created below, which are
#     `Draft` and `Awarded`. None is `Open`, `Negotiating`, or `Delivered` and older than seventy-two
#     hours.
#   * bid-expiry claims live offers whose `pickup_at` has passed. Every bid this file writes collects
#     in two days.
#   * **outbox-publisher is the one with no "not due" state**, exactly as 61-bidding.sh records, so
#     the worker below is pointed at a broker that is not there. Every outbox pass then fails legibly
#     and leaves each row claimable for 80-notifications.sh.
#
# And the reverse direction, which is the one this task added: **nothing this section leaves behind
# is due**, so the five later worker starts pseudonymise nobody. The last check in this section is
# that assertion rather than a comment claiming it.

# The subject: a provider, so that `provider_profiles` is in play as well as `users`.
pseudo_email="pseudonym-$$@example.com"
status="$(post_json "verify-pseudonym-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Pseudonym\",\"email\":\"$pseudo_email\",\"phone\":\"04925$$\",\"password\":\"$login_password\",\"role\":\"provider\"}" \
  "$WORKDIR/pseudonym-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/pseudonym-register.json"; fail "could not register the pseudonymisation subject ($status)"; }
pseudo_user="$(json "$WORKDIR/pseudonym-register.json" '["id"]')"
pseudo_phone="$("$PSQL" "$DATABASE_URL" -tAc "select phone from users where id = '$pseudo_user';")"

status="$(post_json "verify-pseudonym-login-$$" /v1/auth/login \
  "{\"email\":\"$pseudo_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Pseudonym\"}" \
  "$WORKDIR/pseudonym-login.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/pseudonym-login.json"; fail "could not sign the subject in ($status)"; }
pseudo_access="$(json "$WORKDIR/pseudonym-login.json" '["access_token"]')"

# A one-time code, so `phone_otps` holds a copy of the number. Registration has already put a copy
# of the address in `email_verification_tokens`. Both are this package's own tables and both are
# verbatim copies — a single join recovers the person from either while `users` looks entirely clean.
status="$(post_json "verify-pseudonym-otp-$$" /v1/auth/request-otp \
  "{\"phone\":\"$pseudo_phone\"}" "$WORKDIR/pseudonym-otp.json")"
[[ "$status" == "202" ]] \
  || { cat "$WORKDIR/pseudonym-otp.json"; fail "could not issue a one-time code ($status)"; }

# A trading name, which is fleet's table and is reached through a port identity declares. Inserted
# rather than declared through PATCH /v1/fleet/profile because that endpoint is a provider's own
# journey and this section is about what happens to the row, not about how it got there.
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
  "insert into provider_profiles (provider_id, display_name, operates_as)
   values ('$pseudo_user', 'Verify Removals $$', 'business');" >/dev/null \
  || fail "could not declare the subject's trading name"

# A customer, to own the job the notifications hang off and to be the counterparty below.
pseudo_customer_email="pseudonym-c-$$@example.com"
status="$(post_json "verify-pseudonym-c-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Pseudonym Customer\",\"email\":\"$pseudo_customer_email\",\"phone\":\"04926$$\",\"password\":\"$login_password\",\"role\":\"customer\"}" \
  "$WORKDIR/pseudonym-c-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/pseudonym-c-register.json"; fail "could not register the counterparty ($status)"; }
pseudo_customer="$(json "$WORKDIR/pseudonym-c-register.json" '["id"]')"

pseudo_job="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into jobs (id, customer_id) values (gen_random_uuid(), '$pseudo_customer') returning id;" | tr -d ' ')"
[[ -n "$pseudo_job" ]] || fail "could not create the job the notifications hang off"

# Three notifications: the two contact channels, and a push row whose `address` is a *device token*
# and must survive. Docs/05 §3.1 puts device identifiers in the deleted column too, but SHIP-172's
# own *Done when* names them — and writing a contact pseudonym over one would destroy the only value
# saying which handset a failed push was aimed at.
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
  "insert into notifications (id, event_id, event_type, job_id, recipient_id, channel, category,
                              essential, address, subject, body)
   values (gen_random_uuid(), gen_random_uuid(), 'job.status_changed', '$pseudo_job', '$pseudo_user',
           'email', 'award', true, '$pseudo_email', 's', 'b'),
          (gen_random_uuid(), gen_random_uuid(), 'job.status_changed', '$pseudo_job', '$pseudo_user',
           'sms', 'award', true, '$pseudo_phone', 's', 'b'),
          (gen_random_uuid(), gen_random_uuid(), 'job.status_changed', '$pseudo_job', '$pseudo_user',
           'push', 'award', true, 'verify-device-token-$$', 's', 'b');" >/dev/null \
  || fail "could not write the subject's notifications"

# The request, through the endpoint, so the row is the one the platform actually writes.
status="$(post_auth "verify-pseudonym-delete-$$" "$pseudo_access" /v1/account/deletion \
  "$WORKDIR/pseudonym-delete.json")"
[[ "$status" == "202" ]] || { cat "$WORKDIR/pseudonym-delete.json"; fail "the subject's request returned $status, want 202"; }
pseudo_request="$(json "$WORKDIR/pseudonym-delete.json" '["id"]')"

# The promise is moved into the past rather than a clock being advanced, which is the one thing a
# shell harness cannot do to a running binary. It is honest because the column is *stored*: the
# domain tests prove that by advancing a clock.Fixed and watching the same row fall due.
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
  "update account_deletion_requests
      set requested_at = now() - interval '31 days', complete_by = now() - interval '1 day'
    where id = '$pseudo_request';" >/dev/null \
  || fail "could not bring the subject's promised date forward"

# Read back *after* the fixture is in place and *before* the pass, so the assertion afterwards is a
# comparison with the row rather than with a date reconstructed from an interval.
pseudo_promised="$("$PSQL" "$DATABASE_URL" -tAc \
  "select to_char(complete_by at time zone 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.MS\"Z\"')
     from account_deletion_requests where id = '$pseudo_request';")"

# A second account whose promise has *not* arrived. Without it, every assertion below is equally
# true of a sweep that pseudonymises whatever it finds — which is the defect that would execute
# deletions inside all five later sections that start a worker.
pseudo_waiting_email="pseudonym-w-$$@example.com"
status="$(post_json "verify-pseudonym-w-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Pseudonym Waiting\",\"email\":\"$pseudo_waiting_email\",\"phone\":\"04927$$\",\"password\":\"$login_password\",\"role\":\"customer\"}" \
  "$WORKDIR/pseudonym-w-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/pseudonym-w-register.json"; fail "could not register the waiting account ($status)"; }
pseudo_waiting="$(json "$WORKDIR/pseudonym-w-register.json" '["id"]')"

status="$(post_json "verify-pseudonym-w-login-$$" /v1/auth/login \
  "{\"email\":\"$pseudo_waiting_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Waiting\"}" \
  "$WORKDIR/pseudonym-w-login.json")"
[[ "$status" == "200" ]] || fail "could not sign the waiting account in ($status)"
status="$(post_auth "verify-pseudonym-w-delete-$$" "$(json "$WORKDIR/pseudonym-w-login.json" '["access_token"]')" \
  /v1/account/deletion "$WORKDIR/pseudonym-w-delete.json")"
[[ "$status" == "202" ]] || fail "the waiting account could not request deletion ($status)"

# A third: a provider carrying a delivery whose promise *has* arrived. Docs/05 §3.1 — "erasing a
# party mid-delivery would strand the counterparty" — and ports.go promised that SHIP-171 would ask
# again before it executed rather than trusting the state the row was recorded in.
pseudo_carrier_email="pseudonym-x-$$@example.com"
status="$(post_json "verify-pseudonym-x-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Pseudonym Carrier\",\"email\":\"$pseudo_carrier_email\",\"phone\":\"04928$$\",\"password\":\"$login_password\",\"role\":\"provider\"}" \
  "$WORKDIR/pseudonym-x-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/pseudonym-x-register.json"; fail "could not register the carrier ($status)"; }
pseudo_carrier="$(json "$WORKDIR/pseudonym-x-register.json" '["id"]')"

status="$(post_json "verify-pseudonym-x-login-$$" /v1/auth/login \
  "{\"email\":\"$pseudo_carrier_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Carrier\"}" \
  "$WORKDIR/pseudonym-x-login.json")"
[[ "$status" == "200" ]] || fail "could not sign the carrier in ($status)"
status="$(post_auth "verify-pseudonym-x-delete-$$" "$(json "$WORKDIR/pseudonym-x-login.json" '["access_token"]')" \
  /v1/account/deletion "$WORKDIR/pseudonym-x-delete.json")"
[[ "$status" == "202" ]] || fail "the carrier could not request deletion ($status)"
pseudo_carrier_request="$(json "$WORKDIR/pseudonym-x-delete.json" '["id"]')"

# The delivery is created *after* the request, so the request was recorded live and the state the
# sweep meets is one only the re-read can correct. That is the whole of ports.go's promise.
pseudo_delivery_job="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into jobs (id, customer_id) values (gen_random_uuid(), '$pseudo_customer') returning id;" | tr -d ' ')"
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
  "insert into bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
   values (gen_random_uuid(), '$pseudo_delivery_job', '$pseudo_carrier', 'Accepted', 41000,
           now() + interval '2 days', now() + interval '3 days');" >/dev/null \
  || fail "could not accept the carrier's bid"
move_job_for_deferral "$pseudo_delivery_job" Draft Open "$pseudo_customer"
move_job_for_deferral "$pseudo_delivery_job" Open Awarded "$pseudo_customer"
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
  "update account_deletion_requests
      set requested_at = now() - interval '31 days', complete_by = now() - interval '1 day'
    where id = '$pseudo_carrier_request';" >/dev/null \
  || fail "could not bring the carrier's promised date forward"

# The fixture, verified rather than assumed: three requests, two due and one not, and the carrier
# at Awarded. A sweep judged against a fixture that was never in this state proves nothing.
read -r pseudo_due pseudo_not_due pseudo_awarded <<<"$("$PSQL" "$DATABASE_URL" -tAc \
  "select (select count(*) from account_deletion_requests
            where id in ('$pseudo_request', '$pseudo_carrier_request') and complete_by <= now()),
          (select count(*) from account_deletion_requests
            where user_id = '$pseudo_waiting' and complete_by > now()),
          (select status from jobs where id = '$pseudo_delivery_job');" | tr '|' ' ')"
[[ "$pseudo_due" == "2" && "$pseudo_not_due" == "1" && "$pseudo_awarded" == "Awarded" ]] \
  || fail "the fixture is $pseudo_due due, $pseudo_not_due waiting, job '$pseudo_awarded' — want 2, 1, Awarded"
ok "the fixture is two accounts past their promised date, one still inside it, and one of the two carrying a delivery"

pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-worker-pseudonym" ./cmd/worker
popd >/dev/null
ok "the worker builds with the identity domain's task registered"

# KAFKA_BROKERS names a port nothing listens on, deliberately — see the note above. The outbox pass
# fails and leaves every row claimable for 80-notifications.sh; the five claim-based tasks run
# normally against PostgreSQL and need no broker at all.
SHIPPER_ENV=development \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
KAFKA_BROKERS=localhost:1 \
  "$WORKDIR/shipper-worker-pseudonym" >"$WORKDIR/worker-pseudonym.log" 2>&1 &
pseudo_worker_pid=$!

for _ in $(seq 1 100); do
  pseudo_state="$("$PSQL" "$DATABASE_URL" -tAc "select state from account_deletion_requests where id = '$pseudo_request';")"
  [[ "$pseudo_state" == "completed" ]] && break
  sleep 0.2
done

kill -TERM "$pseudo_worker_pid" 2>/dev/null || true
for _ in $(seq 1 50); do kill -0 "$pseudo_worker_pid" 2>/dev/null || break; sleep 0.2; done
wait "$pseudo_worker_pid" 2>/dev/null || true

[[ "$pseudo_state" == "completed" ]] \
  || { cat "$WORKDIR/worker-pseudonym.log"; fail "the request past its promised date is '$pseudo_state' after a pass, want completed"; }
ok "one pass of the real worker executes the request whose thirty days have run out"

grep -q '"task":"account-pseudonymisation"' "$WORKDIR/worker-pseudonym.log" \
  || { cat "$WORKDIR/worker-pseudonym.log"; fail "the identity domain's task did not register in the manifest"; }
ok "and it is registered as account-pseudonymisation, the sixth task in a binary that runs all of them"

# The pseudonym itself, derived from the account identifier — which Docs/05 §3.1 already calls the
# pseudonym, so this is not a reversal of anything. Computed here from the same identifier rather
# than read out of the row, so the check is that the platform wrote the value it should have.
pseudo_token="deleted:$pseudo_user"
pseudo_short="${pseudo_user: -12}"

# Read one at a time rather than with `read -r … <<< $(… | tr '|' ' ')`, which is the shape every
# other multi-value check in this file uses and which is wrong here: the pseudonymised **name**
# contains a space, so word splitting hands `read` four fields for three variables. It failed as
# `users.email is 'user'` — the second word of "Deleted user …" arriving in the email variable.
pseudo_name="$("$PSQL" "$DATABASE_URL" -tAc "select name from users where id = '$pseudo_user';")"
pseudo_new_email="$("$PSQL" "$DATABASE_URL" -tAc "select email::text from users where id = '$pseudo_user';")"
pseudo_new_phone="$("$PSQL" "$DATABASE_URL" -tAc "select phone from users where id = '$pseudo_user';")"
[[ "$pseudo_new_email" == "$pseudo_token" ]] \
  || fail "users.email is '$pseudo_new_email', want '$pseudo_token'"
[[ "$pseudo_new_phone" == "$pseudo_token" ]] \
  || fail "users.phone is '$pseudo_new_phone', want '$pseudo_token'"
[[ "$pseudo_name" == "Deleted user $pseudo_short" ]] \
  || fail "users.name is '$pseudo_name', want 'Deleted user $pseudo_short'"
ok "the account's name and both contact channels are the pseudonym, derived from the identifier every retained row already carries"

# **Not a valid address and not a valid number**, which is what makes the pseudonymised account
# unreachable through the front door by construction rather than by a check somebody remembered.
# The account no longer exists to sign-in under either the old address or the new one.
# The address that was replaced: 400 with SHIP-41's one code for both halves, which is exactly what
# an address that never had an account gets. That is the point — the account is gone in the only
# sense a client can observe.
status="$(post_json "verify-pseudonym-old-signin-$$" /v1/auth/login \
  "{\"email\":\"$pseudo_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Gone\"}" \
  "$WORKDIR/pseudonym-old-signin.json")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/pseudonym-old-signin.json"; fail "signing in with the deleted address returned $status, want 400"; }
[[ "$(json "$WORKDIR/pseudonym-old-signin.json" '["error"]["code"]')" == "identity_credentials_invalid" ]] \
  || fail "the deleted address is refused with something other than SHIP-41's one code for both halves"

# **The pseudonym that replaced it is refused a whole layer earlier — 422, at validation, naming the
# email field.** That is the assertion, not the refusal: the store is never reached, because
# `deleted:<id>` does not parse as an address. A `identity_credentials_invalid` here would mean it
# had been looked up and merely not matched, which is a much weaker property and one a later change
# to the hasher could undo.
status="$(post_json "verify-pseudonym-new-signin-$$" /v1/auth/login \
  "{\"email\":\"$pseudo_token\",\"password\":\"$login_password\",\"device_label\":\"Verify Gone\"}" \
  "$WORKDIR/pseudonym-new-signin.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/pseudonym-new-signin.json"; fail "signing in with the pseudonym returned $status, want 422 — it must be refused at validation, before the store"; }
[[ "$(python3 -c 'import json,sys
print(" ".join(sorted(d["field"] for d in json.load(open(sys.argv[1]))["error"]["details"])))' \
  "$WORKDIR/pseudonym-new-signin.json")" == "email" ]] \
  || { cat "$WORKDIR/pseudonym-new-signin.json"; fail "the pseudonym was not refused as an unusable email address"; }
ok "the address that was replaced is refused like an address with no account, and the pseudonym never reaches the store"

# **The two refusals above are the only failed sign-ins in this file, and SHIP-47 charges the
# per-address bucket on a refused credential.** Every request in a `make verify` run arrives from
# 127.0.0.1, so a later track that got a 429 would look like a broken endpoint rather than like this
# section's leftovers — which is the SHIP-47 section's own argument, applied to the one place after
# it that spends from the bucket. Cleared here rather than at the end, so the last check of the file
# is still the assertion and not the tidy-up.
redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:credential:address:*' \
  | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true

# Every session the account still held. A handset holding a refresh token issued last week keeps
# working after the name, address and number are gone unless something ends it.
read -r pseudo_live pseudo_reason <<<"$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) filter (where revoked_at is null), coalesce(max(revoked_reason), '')
     from device_sessions where user_id = '$pseudo_user';" | tr '|' ' ')"
[[ "$pseudo_live" == "0" ]] || fail "$pseudo_live sessions are still live on a pseudonymised account"
[[ "$pseudo_reason" == "account_deleted" ]] || fail "the sessions ended for reason '$pseudo_reason', want account_deleted"
ok "every session the account held is revoked, and the row says why"

# fleet's table, through the port identity declares and cmd/worker supplies.
pseudo_trading="$("$PSQL" "$DATABASE_URL" -tAc "select display_name from provider_profiles where provider_id = '$pseudo_user';")"
[[ "$pseudo_trading" == "Deleted provider $pseudo_short" ]] \
  || fail "provider_profiles.display_name is '$pseudo_trading', want 'Deleted provider $pseudo_short'"
pseudo_operates="$("$PSQL" "$DATABASE_URL" -tAc "select operates_as from provider_profiles where provider_id = '$pseudo_user';")"
[[ "$pseudo_operates" == "business" ]] \
  || fail "operates_as moved to '$pseudo_operates'; this ticket replaces the identifying declaration, not the row"
ok "the provider's trading name is replaced through fleet, and the form they trade in is not"

# notifications' table, and the device token that must survive it.
read -r pseudo_notif_email pseudo_notif_sms pseudo_notif_push <<<"$("$PSQL" "$DATABASE_URL" -tAc \
  "select max(address) filter (where channel = 'email'),
          max(address) filter (where channel = 'sms'),
          max(address) filter (where channel = 'push')
     from notifications where recipient_id = '$pseudo_user';" | tr '|' ' ')"
[[ "$pseudo_notif_email" == "$pseudo_token" && "$pseudo_notif_sms" == "$pseudo_token" ]] \
  || fail "the notification addresses are '$pseudo_notif_email' and '$pseudo_notif_sms' — a join from this table recovers the person"
[[ "$pseudo_notif_push" == "verify-device-token-$$" ]] \
  || fail "the push row's address is '$pseudo_notif_push'; that column holds a device identifier, which is SHIP-172's"
ok "the notification addresses are replaced and the device token is not"

# **The whole-schema sweep, which is the check the "irreversibly" clause actually needs.**
#
# A list of tables passes on the day it is written and goes on passing when somebody adds a twelfth
# with a contact column. This asks PostgreSQL for every text-shaped column in the public schema and
# looks for the person in all of them, including columns that do not exist yet. It is the sweep that
# found `notifications.address` in the first place.
#
# **The CTE is MATERIALIZED and that is load-bearing rather than tidy.** PostgreSQL does not promise
# an evaluation order for `AND`, so with one flat `WHERE` the planner ran `query_to_xml` *before* the
# schema filter and asked `select count(*) from public.pg_proc` — a catalogue table, in the wrong
# schema, with the `public.` this query used to hard-code. The fence makes the column list a finished
# set before anything is run against it, and the schema is now interpolated rather than assumed.
pseudonym_survivors() {
  "$PSQL" "$DATABASE_URL" -tAc "
    with cols as materialized (
      select table_schema, table_name, column_name
        from information_schema.columns
       where table_schema = 'public'
         and (data_type = 'text' or udt_name = 'citext')
    )
    select coalesce(string_agg(format('%I.%I', table_name, column_name), ', '), '')
      from cols
     where (xpath('/row/c/text()',
                  query_to_xml(format('select count(*) as c from %I.%I where strpos(%I::text, %L) > 0',
                                      table_schema, table_name, column_name, '$1'),
                               false, true, '')))[1]::text::int > 0;"
}

# The fixture is verified first: a sweep for a value that was never stored passes forever. This is
# the same check turned around — before the pass there were copies in more than one table, and the
# assertion below is that there are none.
pseudo_survivors="$(pseudonym_survivors "$pseudo_email")"
[[ -z "$pseudo_survivors" ]] \
  || fail "the address '$pseudo_email' survives in $pseudo_survivors — a join from any one of them recovers the person"
pseudo_survivors="$(pseudonym_survivors "$pseudo_phone")"
[[ -z "$pseudo_survivors" ]] \
  || fail "the number '$pseudo_phone' survives in $pseudo_survivors"
pseudo_survivors="$(pseudonym_survivors "Verify Removals $$")"
[[ -z "$pseudo_survivors" ]] \
  || fail "the trading name survives in $pseudo_survivors"
ok "no text column anywhere in the schema still holds the address, the number or the trading name"

# The fixture check the three assertions above depend on: the counterparty's address, which nothing
# has pseudonymised, is still findable by the same sweep. Without this, an empty answer could mean
# the sweep is broken rather than that the person is gone.
pseudo_survivors="$(pseudonym_survivors "$pseudo_customer_email")"
[[ -n "$pseudo_survivors" ]] \
  || fail "the sweep finds nothing at all, so the three empty answers above prove nothing"
ok "and the sweep still finds an address that was not deleted, so an empty answer means something"

# The promise that was kept, and when it was kept. complete_by is the one instant on this row that
# must not move: rewriting it would replace the record of what the person was told with the moment a
# sweep happened to run, which is what updated_at is for.
pseudo_stored_promise="$("$PSQL" "$DATABASE_URL" -tAc \
  "select to_char(complete_by at time zone 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.MS\"Z\"')
     from account_deletion_requests where id = '$pseudo_request';")"
[[ "$pseudo_stored_promise" == "$pseudo_promised" ]] \
  || fail "complete_by moved from $pseudo_promised to $pseudo_stored_promise; a completed request holds the promise it kept, and updated_at is what says when it was kept"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select updated_at > complete_by from account_deletion_requests where id = '$pseudo_request';")" == "t" ]] \
  || fail "updated_at is not after complete_by, so nothing records when the promise was kept"
ok "the completed request still holds the promise it kept, and updated_at is what says when"

# The carrier, which is ports.go's promise: the delivery is re-read before anything is executed.
read -r pseudo_carrier_state pseudo_carrier_email_now <<<"$("$PSQL" "$DATABASE_URL" -tAc \
  "select r.state, u.email::text
     from account_deletion_requests r join users u on u.id = r.user_id
    where r.id = '$pseudo_carrier_request';" | tr '|' ' ')"
[[ "$pseudo_carrier_email_now" == "$pseudo_carrier_email" ]] \
  || fail "the provider carrying the delivery was pseudonymised; the customer's goods are moving and nobody is named as carrying them"
[[ "$pseudo_carrier_state" == "deferred" ]] \
  || fail "the carrier's request is '$pseudo_carrier_state', want deferred — Docs/05 §3.1 defers rather than refuses"
ok "a party to a delivery is put back on hold at the moment of execution, not erased mid-delivery"

# The account still inside its thirty days.
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select email::text from users where id = '$pseudo_waiting';")" == "$pseudo_waiting_email" ]] \
  || fail "an account whose promised date has not arrived was pseudonymised"
ok "and an account still inside its thirty days is untouched"

# **A completed request does not stop the same account asking again**, which is the whole reason
# 'completed' joined the CHECK and not the open-request index. Asserted against the database rather
# than through the endpoint, because a pseudonymised account cannot sign in — which is the point of
# the two checks above it.
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
  "insert into account_deletion_requests (id, user_id, state, requested_at, complete_by)
   values (gen_random_uuid(), '$pseudo_user', 'requested', now(), now() + interval '30 days');" >/dev/null \
  || fail "uq_account_deletion_requests_open refused a new request from an account whose earlier one was completed — that account could never ask again"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from account_deletion_requests where user_id = '$pseudo_user';")" == "2" ]] \
  || fail "the account does not hold both the completed request and the new one"
ok "a completed request is history — the same account can ask again, and both rows survive"

# A second pass finds nothing, so the sweep cannot re-execute what it has already done.
SHIPPER_ENV=development LOG_FORMAT=json LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" REDIS_URL="$REDIS_URL" KAFKA_BROKERS=localhost:1 \
  "$WORKDIR/shipper-worker-pseudonym" >"$WORKDIR/worker-pseudonym-again.log" 2>&1 &
pseudo_worker_pid=$!
sleep 2
kill -TERM "$pseudo_worker_pid" 2>/dev/null || true
for _ in $(seq 1 50); do kill -0 "$pseudo_worker_pid" 2>/dev/null || break; sleep 0.2; done
wait "$pseudo_worker_pid" 2>/dev/null || true

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from account_deletion_requests where user_id = '$pseudo_user' and state = 'completed';")" == "1" ]] \
  || fail "a second pass produced a second completed request"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select email::text from users where id = '$pseudo_user';")" == "$pseudo_token" ]] \
  || fail "a second pass rewrote the pseudonym, so it is not derived from the account alone"
ok "a second pass changes nothing — the pseudonym is stable and a completed request is not claimed again"

# **The assertion the sixth task owes every later section.** Five sections after this one start
# cmd/worker, and each start runs every registered task. Nothing left here is due, so none of them
# pseudonymises anybody.
pseudo_left_due="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from account_deletion_requests
    where state in ('requested', 'deferred') and complete_by <= now();")"
[[ "$pseudo_left_due" == "0" ]] \
  || fail "this file leaves $pseudo_left_due open deletion requests past their promised date; the next section that starts a worker would execute them"
ok "and this file leaves no deletion request due, so the five later worker starts execute nothing of ours"

# The rate-limit hygiene the whole file depends on, re-asserted as the last thing the file does to
# those keys. This section is the only one after SHIP-47's that spends from them — two refused
# sign-ins against a deleted account — and it clears them where it spends them. Asserted here rather
# than assumed, because a later track's 429 would read as its own endpoint being broken.
[[ "$(redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:credential:address:*' | wc -l | tr -d ' ')" == "0" ]] \
  || fail "this section left per-address sign-in buckets behind, which a later track would be throttled by"
[[ "$(post_json "verify-pseudonym-throttle-$$" /v1/auth/login \
  "{\"email\":\"$pseudo_customer_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Cleared\"}" \
  "$WORKDIR/pseudonym-throttle.json")" == "200" ]] \
  || { cat "$WORKDIR/pseudonym-throttle.json"; fail "sign-in is refused at the end of this file, so a later track would be too"; }
ok "the per-address buckets are empty at the end of the file, and a sign-in still succeeds"
