# shellcheck shell=bash
#
# SHIP-137 — the notification consumer service.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 80–89 is the notifications range (M5). This runs after 80-notifications.sh because that file is
# where the outbox is drained onto the topics this consumer reads.
#
# **The reason used to be a structural one and is no longer true (SHIP-134a).** It read: "that file
# ends by deleting and reapplying the topic set to prove a drifted topic is reported rather than
# repaired — so `shipper.delivery` does not exist for part of it, and a consumer subscribed to every
# topic in the catalogue has to start after that is over". The deletion is gone: the drift refusal
# is now demonstrated by asking `cmd/topics` for a replication factor this single-broker stack
# cannot be holding, which touches no topic at all. There is no longer a window in which a catalogue
# topic is absent. The ordering stays because this section needs the worker run above to have
# happened, which is a data dependency rather than a structural one.
#
# # What is demonstrated here and what is demonstrated by tests
#
# internal/notifications/*_test.go holds the routing, the resolution, the two suppressions, the
# dispatcher's per-channel choice, the retry of a failed send and the two-dispatcher race, all
# against a real PostgreSQL. migrations/notifications_test.go holds the unique index that makes the
# consumer idempotent and the two enumerations against their CHECK constraints.
#
# None of that needs a broker, and none of it is repeated. What only a real cluster can show is the
# chain end to end: a domain event written into the outbox by the platform, published onto a topic
# by cmd/worker, read back off it by a separate binary that resolves a recipient it was never told
# about, and a message reaching an address — and then the same event delivered a second time
# reaching nobody twice.
#
# # Fencing, and the two fences this section needs
#
# **On Kafka, fence by identifier and never by time.** The broker is shared by every worktree with
# no isolation possible, so another run's messages are on these topics and are not ordered against
# this one. Every assertion below is keyed to `$notif_event_id`, the outbox row this section wrote.
#
# **The consumer group is the second fence, and it is the one that is new.** A Kafka consumer group
# is cluster-scoped exactly as a database template name is: two worktrees running `make verify` at
# once would join the same group, be handed different partitions, and each consume the messages the
# other was asserting on — a false failure on a tree where nothing is wrong, which is the shape this
# harness has now paid for three times. `cmd/notifier -group` takes a per-run name here and the
# default everywhere else; consumer.go says why it is a flag rather than configuration.
#
# The group being new per run also means it starts at the beginning of every topic, so it reads
# whatever else is there — this run's own bid and delivery events, and other worktrees'. That is
# harmless and is worth stating rather than working around: events about jobs this database does not
# hold resolve to nobody, and the assertions are on one identifier.
#
# # Owning what this asserts about, before the worker starts
#
# This section starts cmd/worker to publish its event, which runs all five registered tasks. Its one
# job is a Draft, so `job-expiry`, `job-expiry-warning` and `job-auto-complete` find nothing due;
# `bid-expiry` finds nothing this section created. `outbox-publisher` drains whatever is
# unpublished, which at this point in the run is what this section just wrote plus anything later
# sections have not created yet — and 80-notifications.sh, the only file that asserts on the outbox
# or on a topic, has already finished.

ticket "SHIP-137  the consumer reads events, resolves recipients, and dispatches per channel"

# --- a customer, a job, and an event about it ----------------------------------------------------

# 0418x is the notifications range. 04180 is SHIP-134's in 80-notifications.sh; this file takes
# 04181 and 04182, and the range is recorded here because the harness has no other list of them.
status="$(post_json "verify-notif-cust-$$" /v1/auth/register \
  "{\"email\":\"notified-customer-$$@example.com\",\"phone\":\"04181$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/notif-customer.json")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/notif-customer.json"; fail "could not register the notified customer: $status"; }
notif_customer_id="$(json "$WORKDIR/notif-customer.json" '["id"]')"
notif_customer_email="notified-customer-$$@example.com"
notif_token="$(mint_token "$notif_customer_id")"

status="$(curl -s -X POST -o "$WORKDIR/notif-job.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $notif_token" -H "Idempotency-Key: verify-notif-job-$$" \
  -H 'Content-Type: application/json' \
  -d '{"goods_description": "A crate of laboratory glassware"}' \
  "http://localhost:$VERIFY_PORT/v1/jobs")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/notif-job.json"; fail "could not create the job the event is about: $status"; }
notif_job="$(json "$WORKDIR/notif-job.json" '["id"]')"

# The event, written with psql — which is exactly what the contract says a domain does, and is what
# 80-notifications.sh does for three of its four events. A real transition would emit one too, and
# the auto-completion in 51-jobs-autocomplete.sh does; writing it here keeps this section from
# depending on that one having run.
#
# `to: Completed` is chosen because it is the transition nothing else announces:
# notifications.StatusRules routes most of job.status_changed to nobody, on the ground that a job
# reaching Awarded already emitted bid.accepted and a milestone already emitted
# delivery.milestone_recorded. Completed is announced by this and nothing else — and it is where
# SHIP-119's seventy-two hour sweep arrives.
#
# The actor is the platform, so nobody is suppressed: notifications.resolve does not tell somebody
# what they have just done, and a customer-initiated completion would legitimately reach only the
# provider.
# -q as well as -tA: without it psql prints its "INSERT 0 1" command tag on a second line, and
# `tr -d ' '` then glues it onto the identifier as INSERT01. A SELECT has no tag, which is why every
# other capture in this harness gets away with -tAc alone.
notif_event_id="$("$PSQL" "$DATABASE_URL" -tAqc "
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at)
VALUES (gen_random_uuid(), 'job', '$notif_job', 'job.status_changed',
        jsonb_build_object(
            'schema_version', 1,
            'job_id', '$notif_job',
            'from', 'Delivered',
            'to', 'Completed',
            'actor_type', 'system',
            'reason', 'The delivery was not disputed within 72 hours (Docs/02 §6.1).',
            'actor_recorded_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SSZ'),
            'server_recorded_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SSZ')),
        now())
RETURNING id;" | tr -d ' ' | head -1)"
[[ -n "$notif_event_id" ]] || fail "could not write the event the consumer is meant to read"
ok "a job.status_changed event is in the outbox, about a job whose customer the payload never names"

# --- published onto the topic by the real publisher ----------------------------------------------

pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-worker-notif" ./cmd/worker
go build -o "$WORKDIR/shipper-notifier" ./cmd/notifier
popd >/dev/null
ok "cmd/notifier builds as a binary of its own, alongside cmd/worker"

# publish_outbox — one short worker run, which drains whatever is unpublished.
publish_outbox() {
  local log="$1" waiting="${2:-$notif_event_id}" pid
  SHIPPER_ENV=development \
  LOG_FORMAT=json \
  LOG_LEVEL=info \
  DATABASE_URL="$DATABASE_URL" \
  REDIS_URL="$REDIS_URL" \
  KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:29092}" \
    "$WORKDIR/shipper-worker-notif" >"$log" 2>&1 &
  pid=$!
  for _ in $(seq 1 50); do
    [[ "$("$PSQL" "$DATABASE_URL" -tAc \
        "select published_at is not null from outbox where id = '$waiting';")" == "t" ]] && break
    sleep 0.2
  done
  kill -TERM "$pid" 2>/dev/null || true
  for _ in $(seq 1 50); do kill -0 "$pid" 2>/dev/null || break; sleep 0.2; done
  wait "$pid" 2>/dev/null || true
}

publish_outbox "$WORKDIR/notif-publish.log"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select published_at is not null from outbox where id = '$notif_event_id';")" == "t" ]] \
  || { cat "$WORKDIR/notif-publish.log"; fail "the event was never published onto shipper.job"; }
ok "and cmd/worker publishes it onto shipper.job, which is the only thing the consumer reads"

# --- the consumer, in its own process -------------------------------------------------------------

# A group of its own, per run. See the header: a group is cluster-scoped and the broker is shared
# across every worktree, so the default name would have two concurrent runs eating each other's
# messages.
notif_group="shipper-notifications-verify-$$"

# run_notifier <logfile> <predicate-sql> — run the consumer until the predicate is true, then stop.
#
# Stopped before anything is asserted, so a failed assertion cannot leave a consumer running and
# holding a group membership.
#
# **The budget is two minutes rather than the thirty seconds this file's other waits use, and the
# reason is `shipper.bid`.** A group of its own starts at the beginning of every topic, and that
# topic is deliberately padded to eighteen hundred messages as the standing demonstration that
# SHIP-15t's run-start fence works. The consumer has to walk all of it before it is caught up —
# most of those events are about jobs this database does not hold and resolve to nobody, which is
# cheap, but a hundred at a time is still eighteen transactions. Only the first call pays it; the
# rest resume on a committed offset.
run_notifier() {
  local log="$1" predicate="$2" pid
  SHIPPER_ENV=development \
  LOG_FORMAT=json \
  LOG_LEVEL=info \
  DATABASE_URL="$DATABASE_URL" \
  REDIS_URL="$REDIS_URL" \
  KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:29092}" \
    "$WORKDIR/shipper-notifier" -group "$notif_group" >"$log" 2>&1 &
  pid=$!
  for _ in $(seq 1 600); do
    [[ "$("$PSQL" "$DATABASE_URL" -tAc "$predicate")" == "t" ]] && break
    sleep 0.2
  done
  kill -TERM "$pid" 2>/dev/null || true
  for _ in $(seq 1 75); do kill -0 "$pid" 2>/dev/null || break; sleep 0.2; done
  wait "$pid" 2>/dev/null || true
}

notif_sent_sql="select count(*) = 1 from notifications
                 where event_id = '$notif_event_id' and status = 'sent';"

run_notifier "$WORKDIR/notifier.log" "$notif_sent_sql"

notif_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select recipient_id || ' ' || channel || ' ' || category || ' ' || essential || ' ' ||
          address || ' ' || status || ' ' || attempts || ' ' || (sent_at is not null)
     from notifications where event_id = '$notif_event_id';")"
[[ -n "$notif_row" ]] \
  || { cat "$WORKDIR/notifier.log"; fail "the consumer wrote no notification for $notif_event_id"; }

notif_want="$notif_customer_id email award true $notif_customer_email sent 1 true"
[[ "$notif_row" == "$notif_want" ]] \
  || { cat "$WORKDIR/notifier.log"; fail "the notification is '$notif_row', want '$notif_want'"; }
ok "the consumer resolved the job's customer, wrote one row, and dispatched it on the email channel"

# The recipient is the point of this assertion rather than a detail of it. `job.status_changed`
# carries an actor and a job identifier and names no customer at all, so the only way to reach the
# right person is the Parties port — cmd/notifier's jobPartiesLookup, joining `jobs` to `bids`,
# which is a query internal/notifications may not make and does not contain.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select payload::text from outbox where id = '$notif_event_id';")" != *"$notif_customer_id"* ]] \
  || fail "the event names the customer, so this proves nothing about resolution"
ok "and it resolved them from the job rather than from the payload, which never names them"

grep -q "\"to\":\"$notif_customer_email\"" "$WORKDIR/notifier.log" \
  || { cat "$WORKDIR/notifier.log"; fail "no message was handed to the email channel for $notif_customer_email"; }
ok "the message reached the console email adapter, which is what SHIPPER_ENV=development selects"

# --- the body carries nothing it should not -------------------------------------------------------
#
# Docs/01 §4.4's rule: no address, goods description or full customer name in a notification body.
# **SHIP-141 is now a ticket of its own with its own section below**, and this check is the reason
# that section could be written cheaply — it was already reading a real message rather than a unit
# test's fixture. The goods description on this job was chosen to be unmistakable.
#
# What holds it is two things rather than one. The structural half is that the renderer is handed a
# fixed headline and a job identifier and never sees the job; the word-level half is SHIP-141's, and
# it exists because a headline is free prose in a Go literal that no structural guard covers.
notif_body="$("$PSQL" "$DATABASE_URL" -tAc \
  "select body from notifications where event_id = '$notif_event_id';")"
case "$notif_body" in
  *glassware*|*laboratory*) fail "the notification body carries the goods description: $notif_body" ;;
  *budget*|*price*)         fail "the notification body carries a price: $notif_body" ;;
esac
[[ "$notif_body" == *"$notif_job"* ]] \
  || fail "the notification body does not name the job it is about: $notif_body"
ok "the body names the job and carries no goods description, address or price"

# --- delivered twice, told once --------------------------------------------------------------------
#
# The heart of the ticket. internal/notifications/doc.go: "delivery is at least once; every consumer
# is idempotent, because the alternative to a duplicate notification is a missing one" — and
# cmd/worker/outbox.go names the window that makes a duplicate certain rather than possible: the
# publish succeeds, the commit does not, and the next pass republishes the same event id.
#
# That window is reproduced exactly here, by clearing published_at and running the publisher again.
# The same event, with the same id, lands on the topic a second time at a new offset, so the
# consumer genuinely reads it again rather than skipping it on a committed offset.
"$PSQL" "$DATABASE_URL" -q -c \
  "update outbox set published_at = null where id = '$notif_event_id';" >/dev/null

# A second, different event about the same job, written at the same time. It is the marker this
# section waits on, and it works because it cannot overtake the redelivery: both carry the same
# aggregate id, the publisher orders by (occurred_at, id) and the producer keys on the aggregate, so
# the two land on one partition in that order. When the marker's notification is sent, the
# redelivered event has certainly been read.
#
# Waiting on a marker rather than on a timeout is the difference between a check and a sleep. There
# is nothing to wait *for* in the redelivery itself, because the correct outcome is that nothing
# happens.
notif_marker_id="$("$PSQL" "$DATABASE_URL" -tAqc "
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at)
VALUES (gen_random_uuid(), 'job', '$notif_job', 'job.expiry_warned',
        jsonb_build_object(
            'schema_version', 1,
            'job_id', '$notif_job',
            'customer_id', '$notif_customer_id',
            'expires_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SSZ'),
            'warned_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SSZ')),
        now())
RETURNING id;" | tr -d ' ' | head -1)"

publish_outbox "$WORKDIR/notif-republish.log" "$notif_marker_id"
ok "the same event is republished onto the topic, which is the at-least-once window the outbox names"

notif_before="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where event_id = '$notif_event_id';")"

run_notifier "$WORKDIR/notifier-again.log" \
  "select count(*) = 1 from notifications where event_id = '$notif_marker_id' and status = 'sent';"

notif_after="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where event_id = '$notif_event_id';")"
[[ "$notif_before" == "1" && "$notif_after" == "1" ]] \
  || { cat "$WORKDIR/notifier-again.log"; fail "the redelivered event took the count from $notif_before to $notif_after; every extra row is a second email to the same person"; }
ok "a redelivered event writes no second row — uq_notifications_event_recipient_channel refuses it"

# One message went out on this run, and it is the marker rather than the redelivery. The expiry
# warning is the one category Docs/01 §4.5 does not list as essential, which is the whole of what
# SHIP-142 will have to switch off.
[[ "$(grep -c "\"to\":\"$notif_customer_email\"" "$WORKDIR/notifier-again.log" || true)" == "1" ]] \
  || { cat "$WORKDIR/notifier-again.log"; fail "the redelivery sent a second copy of the completion message"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select category || ' ' || essential from notifications where event_id = '$notif_marker_id';")" \
  == "job_expiry false" ]] \
  || fail "the expiry warning is not the mutable category SHIP-142 will switch off"
ok "and sends nothing a second time — the one message on that run is the new event, not the old one"

# --- per channel, not per event --------------------------------------------------------------------
#
# The dispatcher has no list of event types and no opinion about categories: it reads the `channel`
# column and calls the sender that column names. Nothing routes to SMS today — Docs/01 §4.5 names
# push and email, and choosing to text somebody would be product design this ticket did not do — so
# the row is written by hand, which is the only honest way to show the second channel works.
notif_sms_event="$(uuidgen | tr 'A-Z' 'a-z')"
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c "
INSERT INTO notifications
    (id, event_id, event_type, job_id, recipient_id, channel, category, essential,
     address, subject, body)
VALUES (gen_random_uuid(), '$notif_sms_event', 'job.status_changed', '$notif_job',
        '$notif_customer_id', 'sms', 'award', true, '+61400081$$'::text,
        'A job has been completed.', 'A job has been completed and is now closed.');" >/dev/null

run_notifier "$WORKDIR/notifier-sms.log" \
  "select count(*) = 1 from notifications where event_id = '$notif_sms_event' and status = 'sent';"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select status from notifications where event_id = '$notif_sms_event';")" == "sent" ]] \
  || { cat "$WORKDIR/notifier-sms.log"; fail "the SMS notification was never dispatched"; }
grep -q '"msg":"sms (console, not sent)"' "$WORKDIR/notifier-sms.log" \
  || { cat "$WORKDIR/notifier-sms.log"; fail "nothing was handed to the SMS channel"; }
ok "a row naming another channel dispatches through that channel's adapter, with no event-type branch anywhere"

# ---------------------------------------------------------------------------------------
ticket "SHIP-139  push dispatches to iOS and Android and handles token rejection"

# **No Firebase project exists and no service-account key may be committed**, so nothing below
# reaches Google. What is demonstrated here is everything on this side of Google's door: that a
# rule now routes to push, that a row is addressed to a real registered handset, that the adapter
# is handed it, and that a rejection deregisters the device instead of failing the dispatch.
#
# internal/platform/push/fcm_test.go holds the other half against an httptest server standing in
# for a project — the request FCM would receive, and what each of its answers does to a token,
# including all three of its rejection codes. Neither half is repeated in the other.
#
# The credential exchange is the one thing neither can show: FCM's HTTP v1 API takes a short-lived
# OAuth token exchanged from a service-account key, and that exchange needs
# golang.org/x/oauth2/google — a module, and therefore a go.mod change this branch may not make.
# Docs/11 §3 records it as named rather than narrowed away.

# --- a real device session, because a device token binds to one --------------------------------

# **Registered and signed in for real rather than with mint_token.** The harness's minted token
# carries a random `sid`, and a device token's foreign key names an actual `device_sessions` row —
# so a minted token is exactly the case this endpoint refuses. Signing in is what produces the
# session the whole ticket is about, and doing it here is also what lets the sign-out below be a
# real revocation rather than an UPDATE.
push_email="pushed-customer-$$@example.com"
push_password="correct-horse-battery-staple"

status="$(post_json "verify-push-reg-$$" /v1/auth/register \
  "{\"email\":\"$push_email\",\"phone\":\"04183$$\",\"password\":\"$push_password\",\"role\":\"customer\"}" \
  "$WORKDIR/push-customer.json")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/push-customer.json"; fail "could not register the pushed customer: $status"; }
push_customer_id="$(json "$WORKDIR/push-customer.json" '["id"]')"

status="$(post_json "verify-push-login-$$" /v1/auth/login \
  "{\"email\":\"$push_email\",\"password\":\"$push_password\",\"device_label\":\"Verify Pixel 9\"}" \
  "$WORKDIR/push-login.json")"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/push-login.json"; fail "could not sign the pushed customer in: $status"; }
push_access="$(json "$WORKDIR/push-login.json" '["access_token"]')"
push_refresh="$(json "$WORKDIR/push-login.json" '["refresh_token"]')"

push_request() {
  curl -s -X "$1" -o "$4" -w '%{http_code}' \
    -H "$auth_header: Bearer $push_access" \
    -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' \
    ${5:+-d "$5"} "http://localhost:$VERIFY_PORT$3"
}

# --- SHIP-140: the registration, and what it binds to -------------------------------------------

push_token_value="verify-fcm-token-$$"

status="$(push_request POST "verify-push-token-$$" /v1/notifications/device-tokens \
  "$WORKDIR/push-token.json" "{\"token\":\"$push_token_value\",\"platform\":\"android\"}")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/push-token.json"; fail "registering a device token returned $status, want 201"; }

# The response does not echo the token. It identifies somebody's handset and the client already
# has it; a response body is logged by more middleware than anybody remembers.
grep -q "$push_token_value" "$WORKDIR/push-token.json" \
  && fail "the registration echoed the device token back in its response body"
ok "a handset registers for push and is answered without its token being repeated"

# The binding, read through `device_sessions` — which is what makes the sign-out below work with
# nothing writing to `device_tokens` at all.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_tokens t
     join device_sessions s on s.id = t.device_session_id
    where t.token = '$push_token_value' and t.revoked_at is null
      and s.user_id = '$push_customer_id' and s.revoked_at is null;")" == "1" ]] \
  || fail "the device token is not bound to a live device session belonging to the caller"
ok "the token binds to the device session its credential was issued against, not to the account"

# Registering again is the ordinary case: Firebase hands the app a token at every launch and only
# sometimes the same one.
push_token_second="verify-fcm-token-second-$$"
status="$(push_request POST "verify-push-token-2-$$" /v1/notifications/device-tokens \
  "$WORKDIR/push-token-2.json" "{\"token\":\"$push_token_second\",\"platform\":\"android\"}")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/push-token-2.json"; fail "re-registering returned $status, want 201"; }

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_tokens t
     join device_sessions s on s.id = t.device_session_id
    where s.user_id = '$push_customer_id' and t.revoked_at is null;")" == "1" ]] \
  || fail "the handset holds more than one live token; every notification to it would be two"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select revoked_reason from device_tokens where token = '$push_token_value';")" == "replaced" ]] \
  || fail "the displaced token was not revoked as replaced"
ok "a second registration from one handset leaves exactly one live token, and says why the first went"

# A platform nothing ships on is a validation failure rather than a constraint violation, which is
# the difference between a field error and a 500 with a constraint name in it.
status="$(push_request POST "verify-push-bad-platform-$$" /v1/notifications/device-tokens \
  "$WORKDIR/push-bad-platform.json" '{"token":"x","platform":"windows-phone"}')"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/push-bad-platform.json"; fail "an unknown platform returned $status, want 422"; }
[[ "$(json "$WORKDIR/push-bad-platform.json" '["error"]["details"][0]["field"]')" == "platform" ]] \
  || { cat "$WORKDIR/push-bad-platform.json"; fail "the field error does not name platform"; }
ok "a platform this app does not ship on is a field error, not a constraint violation"

# Every state-changing endpoint is refused without an idempotency key (SHIP-15).
status="$(curl -s -X POST -o "$WORKDIR/push-no-key.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $push_access" -H 'Content-Type: application/json' \
  -d "{\"token\":\"x\",\"platform\":\"ios\"}" \
  "http://localhost:$VERIFY_PORT/v1/notifications/device-tokens")"
[[ "$status" == "400" ]] \
  || { cat "$WORKDIR/push-no-key.json"; fail "a registration with no Idempotency-Key returned $status"; }
ok "and it is refused without an idempotency key, like every other state-changing endpoint"

# --- a push row, addressed to that handset, handed to the adapter -------------------------------

push_event_id="$("$PSQL" "$DATABASE_URL" -tAqc "
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at)
VALUES (gen_random_uuid(), 'job', '$notif_job', 'bid.placed',
        jsonb_build_object(
            'schema_version', 1,
            'job_id', '$notif_job',
            'bid_id', gen_random_uuid(),
            'provider_id', gen_random_uuid()),
        now())
RETURNING id;" | tr -d ' ' | head -1)"
[[ -n "$push_event_id" ]] || fail "could not write the event the push is about"

# The job belongs to the *other* customer in this file, so the notification would go to them and
# not to the handset just registered. Point the job at this customer for the duration: recipient
# resolution is cmd/notifier's Parties query over `jobs`, and this is the shortest honest way to
# make the registered handset the audience without inventing a second job.
"$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 \
  -c "UPDATE jobs SET customer_id = '$push_customer_id' WHERE id = '$notif_job';" >/dev/null

publish_outbox "$WORKDIR/push-publish.log" "$push_event_id"
run_notifier "$WORKDIR/notifier-push.log" \
  "select count(*) > 0 from notifications where event_id = '$push_event_id' and channel = 'push' and status = 'sent';"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select address from notifications where event_id = '$push_event_id' and channel = 'push';")" \
  == "$push_token_second" ]] \
  || { cat "$WORKDIR/notifier-push.log"; fail "no push row was addressed to the registered handset"; }
ok "a rule routes to push and the row is addressed to the device token, not to the account"

# The adapter was handed it. In development that is push.Noop, which records and never rejects —
# a no-op that reported a rejection would deregister real devices on the strength of nothing.
grep -q '"msg":"push (noop, not sent)"' "$WORKDIR/notifier-push.log" \
  || { cat "$WORKDIR/notifier-push.log"; fail "nothing was handed to the push channel"; }

# And the token is fingerprinted rather than logged whole. It names one person's handset, and a
# development log is the least protected place in this system.
grep -q "\"device\":\"$push_token_second\"" "$WORKDIR/notifier-push.log" \
  && fail "the device token is in the log in full"
ok "the push adapter received it, and logged a fingerprint of the handset rather than its token"

# The email went out alongside it, because Docs/01 §4.5 keeps email for the record.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where event_id = '$push_event_id' and channel = 'email' and status = 'sent';")" == "1" ]] \
  || fail "the event produced no email beside the push"
ok "and the same event still produces the email Docs/01 §4.5 keeps for the record"

# ---------------------------------------------------------------------------------------
ticket "SHIP-140  tokens bind to a device session and clear on sign-out"

# **The clause the whole design turns on, demonstrated against a real revocation.**
#
# 000104 revokes a device session rather than deleting it, so a foreign key cascade would never
# fire. What clears the token instead is that nothing resolves a push address whose session is not
# live — so signing out ends delivery from the instant it commits, with no cross-domain write and
# no change to internal/identity.
#
# The two halves are asserted separately below because either alone would be misleading: that no
# push is written, and that the `device_tokens` row is exactly as it was.
push_rows_before="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_tokens t join device_sessions s on s.id = t.device_session_id
    where s.user_id = '$push_customer_id' and t.revoked_at is null;")"

status="$(curl -s -X POST -o "$WORKDIR/push-logout.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $push_access" -H "Idempotency-Key: verify-push-logout-$$" \
  "http://localhost:$VERIFY_PORT/v1/auth/logout")"
[[ "$status" == "204" ]] \
  || { cat "$WORKDIR/push-logout.json"; fail "signing out returned $status, want 204"; }

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_sessions where user_id = '$push_customer_id' and revoked_at is not null;")" == "1" ]] \
  || fail "the sign-out did not revoke the device session"

push_after_event="$("$PSQL" "$DATABASE_URL" -tAqc "
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at)
VALUES (gen_random_uuid(), 'job', '$notif_job', 'bid.revised',
        jsonb_build_object(
            'schema_version', 1,
            'job_id', '$notif_job',
            'bid_id', gen_random_uuid(),
            'provider_id', gen_random_uuid()),
        now())
RETURNING id;" | tr -d ' ' | head -1)"

publish_outbox "$WORKDIR/push-after-publish.log" "$push_after_event"
run_notifier "$WORKDIR/notifier-after.log" \
  "select count(*) = 1 from notifications where event_id = '$push_after_event' and channel = 'email' and status = 'sent';"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where event_id = '$push_after_event' and channel = 'push';")" == "0" ]] \
  || { cat "$WORKDIR/notifier-after.log"; fail "a push was addressed to a handset whose session had been signed out"; }
ok "signing out ends push delivery to that handset, from the instant the revocation commits"

push_rows_after="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_tokens t join device_sessions s on s.id = t.device_session_id
    where s.user_id = '$push_customer_id' and t.revoked_at is null;")"
[[ "$push_rows_before" == "$push_rows_after" ]] \
  || fail "signing out wrote to device_tokens; the guarantee is meant to need no cross-domain write"
ok "and it wrote nothing to device_tokens — the token is unreachable rather than deleted"

# The email still went out, which is what says the account was still notified rather than the
# consumer having silently stopped.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where event_id = '$push_after_event' and channel = 'email';")" == "1" ]] \
  || fail "the signed-out customer was not emailed either, so this proves nothing about push"
ok "the account is still emailed, so the absent push is the session rule and not a dead consumer"

# ---------------------------------------------------------------------------------------
ticket "SHIP-138  each essential event has an email template and sends reliably"

# The copy, read off the row the dispatcher sent. internal/notifications/rules_test.go holds every
# template against its source byte-for-byte; what only this can show is that what a real event
# produces, through a real consumer, is that same fixed text and a job identifier.
push_body="$("$PSQL" "$DATABASE_URL" -tAc \
  "select body from notifications where event_id = '$push_after_event' and channel = 'email';")"

grep -q "This message is about job $notif_job" <<<"$push_body" \
  || { printf '%s\n' "$push_body"; fail "the email body does not name the job it is about"; }
grep -q "please do not reply" <<<"$push_body" \
  || { printf '%s\n' "$push_body"; fail "the email body has no closing"; }
ok "an essential event's email carries its template's copy and the job it concerns"

# SHIP-141's rule, on the bytes that left the building. The renderer is handed a headline the
# routing table declares as a literal and a job identifier, and the template's input struct has no
# third field — so this is a check that the structure held, not a redaction pass.
notif_goods="A crate of laboratory glassware"
if grep -qi "$notif_goods" <<<"$push_body"; then
  fail "the goods description reached a notification body"
fi
if grep -qi "$push_email" <<<"$push_body"; then
  fail "an address reached a notification body"
fi
ok "and it carries no goods description and no address, because the renderer never reads the job"

# The reliability half. A row that failed waits before it is claimed again — without which twenty
# permanently failing rows are claimed on every pass forever and nothing written after them is
# ever sent.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from information_schema.columns
    where table_name = 'notifications' and column_name = 'next_attempt_at';")" == "1" ]] \
  || fail "notifications has no next_attempt_at, so one bad address stops the queue"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from pg_indexes where indexname = 'idx_notifications_undelivered'
     and indexdef like '%undeliverable%';")" == "1" ]] \
  || fail "the claim index does not exclude the terminal statuses, so the claim is not served by it"
ok "a failed notification is deferred rather than reclaimed immediately, and the claim's index says so"

# --- deregistration, which is a courtesy rather than the control --------------------------------
#
# The session is already revoked above, so this is the client's own tidy-up arriving after the
# platform has already stopped addressing the handset. It answers 204 either way: a client retrying
# after a dropped connection must not be told it did something wrong.
status="$(curl -s -X DELETE -o "$WORKDIR/push-deregister.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $push_access" -H "Idempotency-Key: verify-push-dereg-$$" \
  "http://localhost:$VERIFY_PORT/v1/notifications/device-tokens/current")"
[[ "$status" == "204" || "$status" == "401" ]] \
  || { cat "$WORKDIR/push-deregister.json"; fail "deregistering returned $status, want 204 or 401"; }
ok "deregistering answers without complaint, whether or not anything was still registered ($status)"

# ---------------------------------------------------------------------------------------
ticket "SHIP-141  no address, goods description, or full customer name appears in a notification body"

# The redaction rules applied to **rows a real consumer wrote**, which is the one thing no test in
# internal/notifications can do.
#
# That package's tests run the guard over the routing table and over synthetic copy; both are the
# platform checking its own literals. This runs it over what is in the `notifications` table after a
# real job, a real event and a real dispatch — the same text the console adapter handed out.
#
# **The digit rule is the one worth doing here**, because it is the rule that does not need a
# vocabulary. Every street number, unit, postcode, weight, quantity and amount is a digit, and the
# job identifier is the only number a notification may carry — so the check is "remove the
# identifiers, then find a digit", exactly as internal/notifications/redaction.go does it.
notif_leaks="$("$PSQL" "$DATABASE_URL" -tAc "
  select count(*) from notifications
   where recipient_id in ('$notif_customer_id', '$push_customer_id')
     and regexp_replace(subject || ' ' || body,
           '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}',
           '', 'g') ~ '[0-9]';")"
[[ "$notif_leaks" == "0" ]] \
  || fail "$notif_leaks notification(s) carry a digit that is not the job identifier — a street number, a postcode, a weight or an amount"
ok "no notification this run wrote carries a number other than the job identifier"

notif_streets="$("$PSQL" "$DATABASE_URL" -tAc "
  select count(*) from notifications
   where recipient_id in ('$notif_customer_id', '$push_customer_id')
     and (subject || ' ' || body) ~*
         '\\m(street|road|avenue|drive|lane|court|parade|highway|crescent|terrace|boulevard|esplanade)\\M';")"
[[ "$notif_streets" == "0" ]] \
  || fail "$notif_streets notification(s) name a street type, which is an address however it is written"
ok "and none names a street type, which is the rule that catches an address written in lower case"

# The rule that catches a person, applied to what the table actually holds.
#
# A capitalised word that does not start a sentence is a name, a suburb, a street or a business.
# `Shipper` and `Job` are the two exceptions and they are the whole allowlist — anything else here
# is prose somebody wrote into rules.go that nobody read closely enough.
#
# **Subject and body as separate records**, one per line, rather than concatenated. The first
# version of this check joined them with a separator and the separator itself broke the sentence
# scan: the email subject ends `(job <uuid>)`, so the body's first word followed a closing bracket
# rather than a full stop and was reported as a name. The concatenation was an artefact of the
# check; internal/notifications checks the two halves apart, and so does this.
"$PSQL" "$DATABASE_URL" -tAc "
  select replace(subject, E'\n', ' ') from notifications
   where recipient_id in ('$notif_customer_id', '$push_customer_id')
  union all
  select replace(body, E'\n', ' ') from notifications
   where recipient_id in ('$notif_customer_id', '$push_customer_id');" >"$WORKDIR/notif-copy.txt"

python3 - "$WORKDIR/notif-copy.txt" <<'PY' || fail "a notification names a person, a place or a business"
import re, sys

allowed = {"Shipper", "Job"}
word = re.compile(r"[A-Za-z][A-Za-z'-]*")
uuid = re.compile(r"[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}")

for line in open(sys.argv[1]):
    text = uuid.sub("", line.rstrip("\n"))
    for m in word.finditer(text):
        if not m.group(0)[0].isupper() or m.group(0) in allowed:
            continue
        # Scan back over whitespace and the punctuation that can sit after a full stop.
        i = m.start() - 1
        while i >= 0 and text[i] in " \t([\"'*-":
            i -= 1
        if i < 0 or text[i] in ".!?:;":
            continue
        sys.exit("%r is capitalised mid-sentence in: %s" % (m.group(0), text))
PY
ok "and no notification names a person, a place or a business — the rule that catches a leak written as prose"

# The refusal itself, on the path that would actually be taken. Render is the only writer of a
# notification's text, so copy that breaks the rules produces no row rather than a scrubbed one, and
# the sentinel is distinguishable — which is what lets a consumer log it as a defect rather than as
# a transient failure. Run the way 40-identity.sh runs its password tests, for the same reason: the
# claim is about a branch nothing in this harness can reach without writing bad copy into the build.
pushd "$ROOT/services/core" >/dev/null
go test ./internal/notifications -count=1 \
  -run 'TestRenderRefusesCopyThatWouldReachAHandset|TestNoRuleCanRenderCopyThatBreaksTheRedactionRules' \
  >"$WORKDIR/notif-redaction.log" 2>&1 \
  || { cat "$WORKDIR/notif-redaction.log"; fail "the redaction guard does not hold"; }
popd >/dev/null
ok "and a headline carrying an address, a name, a postcode or a link renders nothing at all"

# ---------------------------------------------------------------------------------------
ticket "SHIP-142  a user can mute non-essential categories; essential events cannot be muted"

prefs_request() {
  curl -s -X "$1" -o "$3" -w '%{http_code}' \
    -H "$auth_header: Bearer $notif_token" \
    -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' \
    ${4:+-d "$4"} "http://localhost:$VERIFY_PORT/v1/notifications/preferences"
}

# --- the screen ---------------------------------------------------------------------------------

status="$(prefs_request GET "verify-prefs-get-$$" "$WORKDIR/prefs.json")"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/prefs.json"; fail "reading preferences returned $status, want 200"; }

# Every category rather than only the muted ones, and `essential` served rather than assumed. The
# client holds no category list: Docs/06 §5.3 keeps anything that changes under operational pressure
# on the platform, and Flutter has no over-the-air path for Dart code.
prefs_shape="$(python3 -c '
import json, sys
rows = json.load(open(sys.argv[1]))["categories"]
mutable = [r["category"] for r in rows if not r["essential"]]
muted = [r["category"] for r in rows if r["muted"]]
print("%d %s %s" % (len(rows), ",".join(sorted(mutable)) or "-", ",".join(sorted(muted)) or "-"))
' "$WORKDIR/prefs.json")"
[[ "$prefs_shape" == "4 job_expiry -" ]] \
  || { cat "$WORKDIR/prefs.json"; fail "the preference screen is [$prefs_shape], want '4 job_expiry -'"; }
ok "the screen serves all four categories, marks exactly job_expiry mutable, and starts with nothing muted"

# --- an essential category is refused -------------------------------------------------------------

status="$(prefs_request PUT "verify-prefs-essential-$$" "$WORKDIR/prefs-essential.json" \
  '{"muted": ["job_expiry", "award"]}')"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/prefs-essential.json"; fail "muting an essential category returned $status, want 422"; }
grep -q 'notifications_category_essential' "$WORKDIR/prefs-essential.json" \
  || { cat "$WORKDIR/prefs-essential.json"; fail "the refusal does not carry notifications_category_essential, so a client cannot branch on it"; }
grep -q '"field":"muted.1"' "$WORKDIR/prefs-essential.json" \
  || { cat "$WORKDIR/prefs-essential.json"; fail "the refusal does not name which element was refused"; }
ok "muting an essential category is refused with a code and the index of the value that caused it"

# And it wrote nothing at all — including the mutable category that shared the request. A handler
# validating as it inserts would have left `job_expiry` behind.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notification_preferences where user_id = '$notif_customer_id';")" == "0" ]] \
  || fail "a refused preference request wrote a row for the half of it that was valid"
ok "and a refused request writes nothing, not even the part of it that was allowed"

# The control, rather than the courtesy. The handler's refusal is what gives the client a field
# error; ck_notification_preferences_category is what makes the row impossible however it is
# written, and only a statement that bypasses the service can show that.
if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -c \
  "insert into notification_preferences (user_id, category, muted_at)
   values ('$notif_customer_id', 'award', now());" >/dev/null 2>&1; then
  "$PSQL" "$DATABASE_URL" -q -c \
    "delete from notification_preferences where user_id = '$notif_customer_id' and category = 'award';" >/dev/null
  fail "psql muted an essential category, so the guarantee is a handler branch rather than a constraint"
fi
ok "and an INSERT that never touches the service is refused too — the constraint is the control"

# --- muting the one mutable category ----------------------------------------------------------------

status="$(prefs_request PUT "verify-prefs-mute-$$" "$WORKDIR/prefs-muted.json" \
  '{"muted": ["job_expiry"]}')"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/prefs-muted.json"; fail "muting job_expiry returned $status, want 200"; }
grep -q '"category":"job_expiry","essential":false,"muted":true' "$WORKDIR/prefs-muted.json" \
  || { cat "$WORKDIR/prefs-muted.json"; fail "the response does not show job_expiry muted"; }
ok "muting job_expiry succeeds and the whole screen comes back with it switched off"

# --- what a mute is for ------------------------------------------------------------------------------
#
# Two events written together, and the pairing is what makes the negative assertion sound.
#
#   * a **job.expiry_warned** — the mutable category, and the same event type this same customer
#     already received a notification for earlier in this section, before the mute existed. So "no
#     row" cannot be a routing gap: the identical event wrote one twenty checks ago;
#   * a **job.status_changed to Cancelled** — CategoryAward, which Docs/01 §4.5 lists as essential.
#     It is the marker the consumer is waited on, and it is also the second clause of the *Done
#     when*: this account has a mute in the table and is told anyway.
#
# Waiting on the essential event rather than on a timeout is the difference between a check and a
# sleep. When its notification is sent the consumer has certainly read the expiry warning too: both
# carry the same aggregate id, the publisher orders by (occurred_at, id) and the producer keys on
# the aggregate, so the two land on one partition in the order they were written.
# **Its own job, and this is a trap rather than tidiness.**
#
# `$notif_job` is not this customer's any more. The SHIP-139 section above reassigns it —
# `UPDATE jobs SET customer_id = '$push_customer_id'` — so that a push rule resolves to the handset
# it registered, and the variable keeps its name afterwards. Every rule here resolves its recipient
# through the Parties port, which reads `jobs.customer_id`, so a mute set on `$notif_customer_id`
# would be checked against an account that no longer owns the job and the muted event would be sent.
# That is exactly how this section failed the first time it ran.
prefs_status="$(curl -s -X POST -o "$WORKDIR/prefs-job.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $notif_token" -H "Idempotency-Key: verify-prefs-job-$$" \
  -H 'Content-Type: application/json' \
  -d '{"goods_description": "A crate of laboratory glassware"}' \
  "http://localhost:$VERIFY_PORT/v1/jobs")"
[[ "$prefs_status" == "201" ]] \
  || { cat "$WORKDIR/prefs-job.json"; fail "could not create the job the mute is demonstrated on: $prefs_status"; }
prefs_job="$(json "$WORKDIR/prefs-job.json" '["id"]')"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select customer_id from jobs where id = '$prefs_job';")" == "$notif_customer_id" ]] \
  || fail "the job this section mutes against does not belong to the account holding the mute"

prefs_muted_event="$("$PSQL" "$DATABASE_URL" -tAqc "
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at)
VALUES (gen_random_uuid(), 'job', '$prefs_job', 'job.expiry_warned',
        jsonb_build_object(
            'schema_version', 1,
            'job_id', '$prefs_job',
            'customer_id', '$notif_customer_id',
            'expires_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SSZ'),
            'warned_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SSZ')),
        now())
RETURNING id;" | tr -d ' ' | head -1)"

prefs_essential_event="$("$PSQL" "$DATABASE_URL" -tAqc "
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at)
VALUES (gen_random_uuid(), 'job', '$prefs_job', 'job.status_changed',
        jsonb_build_object(
            'schema_version', 1,
            'job_id', '$prefs_job',
            'from', 'Open',
            'to', 'Cancelled',
            'actor_type', 'system',
            'reason', 'Demonstrating that an essential category is sent to a muted account.',
            'actor_recorded_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SSZ'),
            'server_recorded_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SSZ')),
        now() + interval '1 second')
RETURNING id;" | tr -d ' ' | head -1)"

[[ -n "$prefs_muted_event" && -n "$prefs_essential_event" ]] \
  || fail "could not write the two events the mute is demonstrated with"

publish_outbox "$WORKDIR/prefs-publish.log" "$prefs_essential_event"
run_notifier "$WORKDIR/prefs-notifier.log" \
  "select count(*) >= 1 from notifications where event_id = '$prefs_essential_event';"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where event_id = '$prefs_essential_event';")" != "0" ]] \
  || { cat "$WORKDIR/prefs-notifier.log"; fail "the essential event told nobody, so nothing below proves anything about the mute"; }
ok "an essential event still reaches an account that has a mute in the table — it cannot be switched off"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where event_id = '$prefs_muted_event';")" == "0" ]] \
  || { cat "$WORKDIR/prefs-notifier.log"; fail "the muted category wrote a notification anyway"; }
ok "and the muted category writes no notification at all, on any channel — the identical event wrote one before the mute"

# --- unmuting ------------------------------------------------------------------------------------------
#
# `[]` is "send me everything" and has to be accepted. An implementation reading an empty list as
# "change nothing" fails silently, because the screen it answers with is the one the client sent.
status="$(prefs_request PUT "verify-prefs-unmute-$$" "$WORKDIR/prefs-unmuted.json" '{"muted": []}')"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/prefs-unmuted.json"; fail "unmuting everything returned $status, want 200"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notification_preferences where user_id = '$notif_customer_id';")" == "0" ]] \
  || fail "an empty muted list left a preference row behind, so it was read as 'change nothing'"
ok "an empty list unmutes everything rather than changing nothing, which is the silent failure it would otherwise be"

unset prefs_shape prefs_muted_event prefs_essential_event notif_leaks notif_streets
unset prefs_status prefs_job
unset -f prefs_request

unset push_email push_password push_customer_id push_access push_refresh push_token_value
unset push_token_second push_event_id push_after_event push_body push_rows_before push_rows_after
unset push_goods notif_goods
unset -f push_request

unset notif_customer_id notif_customer_email notif_token notif_job notif_event_id notif_group
unset notif_row notif_want notif_body notif_before notif_after notif_sent_sql notif_sms_event
unset notif_marker_id
unset -f publish_outbox run_notifier
