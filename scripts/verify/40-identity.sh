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
if ! password_log="$(go test ./internal/identity/ -run TestPassword -count=1 -v 2>&1)"; then
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

# Reversibility is a property of the schema as much as of the code: one credential column, and
# it holds a derived key.
credential_column="$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(string_agg(table_name || '.' || column_name, ', ' order by table_name), 'none')
     from information_schema.columns
    where table_schema = 'public'
      and column_name ~ '(password|secret|passphrase)'")"
[[ "$credential_column" == "users.password_hash" ]] \
  || fail "expected users.password_hash and nothing else, found: $credential_column"
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
  "{\"email\":\"$reg_email\",\"phone\":\"$reg_phone_local\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
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
  "{\"email\":\"$reg_email\",\"phone\":\"0499${$}0\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/register-dupe-email.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/register-dupe-email.json"; fail "a duplicate email returned $status, want 409"; }
[[ "$(json "$WORKDIR/register-dupe-email.json" '["error"]["code"]')" == "identity_email_taken" ]] \
  || fail "expected code=identity_email_taken"
ok "a second account on the same address is refused by uq_users_email"

# The same number in the form a person types it rather than the form it is stored in. Without
# normalisation these are two different strings and the index never sees a collision — which is
# the defect that would give one handset two accounts and make an OTP ambiguous.
status="$(post_json "verify-reg-dupe-phone-$$" /v1/auth/register \
  "{\"email\":\"other-$$@example.com\",\"phone\":\"${reg_phone_local:0:4} ${reg_phone_local:4}\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
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
[[ "$fields" == "email password phone role " ]] \
  || fail "the rejected fields are '$fields', want all four at once"
ok "every bad field is reported at once, so the form takes one round trip and not four"

# Registration is public — it is how a caller obtains credentials in the first place — and it is
# still behind the idempotency middleware like every other state-changing request.
status="$(curl -s -X POST -o "$WORKDIR/register-nokey.json" -w '%{http_code}' \
  -H 'Content-Type: application/json' -d '{}' "http://localhost:$VERIFY_PORT/v1/auth/register")"
[[ "$status" == "400" ]] || fail "registration without an Idempotency-Key returned $status"
[[ "$(json "$WORKDIR/register-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || fail "expected code=idempotency_key_required"
ok "it needs an Idempotency-Key, so a retry cannot produce a second account"

# ---------------------------------------------------------------------------------------
ticket "SHIP-45  the role is chosen at registration and cannot be changed afterwards"

status="$(post_json "verify-provider-$$" /v1/auth/register \
  "{\"email\":\"provider-$$@example.com\",\"phone\":\"0498$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/register-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/register-provider.json"; fail "registering a provider returned $status"; }
[[ "$(json "$WORKDIR/register-provider.json" '["role"]')" == "provider" ]] \
  || fail "the account did not take the provider role"
ok "an account is created as either customer or provider, as asked"

status="$(post_json "verify-admin-role-$$" /v1/auth/register \
  "{\"email\":\"admin-$$@example.com\",\"phone\":\"0497$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"admin\"}" \
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
