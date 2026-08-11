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
# script arrives from 127.0.0.1 — so one address bucket is shared by every section below, and by
# every previous run of this script. A run that ended part-way through SHIP-47 would otherwise
# leave it empty and the *next* run would fail here, with a 429 that looks like a broken endpoint.
#
# Cleared once, at the point sign-ins begin, so the run starts from a known state. Nothing above
# this line signs in.
redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:signin:*' \
  | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true

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
  "{\"email\":\"$suspended_email\",\"phone\":\"04960$$\",\"password\":\"$login_password\",\"role\":\"customer\"}" \
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
  "{\"email\":\"$devices_email\",\"phone\":\"04950$$\",\"password\":\"$login_password\",\"role\":\"provider\"}" \
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
# The per-account buckets go with it, so both halves below start from capacity.
redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:signin:*' \
  | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true

throttle_email="throttle-$$@example.com"
status="$(post_json "verify-throttle-register-$$" /v1/auth/register \
  "{\"email\":\"$throttle_email\",\"phone\":\"04940$$\",\"password\":\"$login_password\",\"role\":\"customer\"}" \
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
cleared="$(redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:signin:address:*' \
  | xargs -r redis-cli -u "$REDIS_URL" del 2>/dev/null || true)"
remaining="$(redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:signin:address:*' | wc -l | tr -d ' ')"
[[ "$remaining" == "0" ]] || fail "$remaining per-address buckets survived the clean-up"
status="$(post_json "verify-throttle-cleared-$$" /v1/auth/login \
  "{\"email\":\"$devices_email\",\"password\":\"$login_password\",\"device_label\":\"Verify Cleared\"}" \
  "$WORKDIR/throttle-cleared.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/throttle-cleared.json"; fail "sign-in is still refused after the address buckets were cleared ($status)"; }
ok "the per-address buckets are cleared, so a later section is not throttled by this one ($cleared removed)"
