# shellcheck shell=bash
#
# SHIP-137 — the notification consumer service.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 80–89 is the notifications range (M5). This runs after 80-notifications.sh rather than inside it
# because that file ends by deleting and reapplying the topic set to prove a drifted topic is
# reported rather than repaired — so `shipper.delivery` does not exist for part of it, and a
# consumer subscribed to every topic in the catalogue has to start after that is over.
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
notif_event_id="$("$PSQL" "$DATABASE_URL" -tAc "
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
RETURNING id;" | tr -d ' ')"
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
  for _ in $(seq 1 150); do
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
# SHIP-141 is not this ticket, and the rule it will enforce is Docs/01 §4.4's: no address, goods
# description or full customer name in a notification body. The reason it holds already is
# structural — the renderer is handed a fixed headline and a job identifier and never sees the job —
# and this is that being true of a real message rather than of a unit test's fixture. The goods
# description on this job was chosen to be unmistakable.
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
notif_marker_id="$("$PSQL" "$DATABASE_URL" -tAc "
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at)
VALUES (gen_random_uuid(), 'job', '$notif_job', 'job.expiry_warned',
        jsonb_build_object(
            'schema_version', 1,
            'job_id', '$notif_job',
            'customer_id', '$notif_customer_id',
            'expires_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SSZ'),
            'warned_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SSZ')),
        now())
RETURNING id;" | tr -d ' ')"

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

# --- push is declared and cannot be sent, and that is stated rather than hidden ---------------------
#
# Docs/01 §4.5 makes push the primary channel. SHIP-139 is the Firebase adapter and SHIP-140 the
# device token registry, and neither exists — so there is no address a push row could carry.
# notifications.Rules therefore writes none, notifications.Pusher is declared for SHIP-139 to fill,
# and cmd/notifier passes nil. This check is that the gap is where it is claimed to be rather than
# somewhere a later ticket would trip over.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where channel = 'push';")" == "0" ]] \
  || fail "a push notification was written, and nothing can send one until SHIP-139 and SHIP-140"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from pg_constraint
    where conname = 'ck_notifications_channel' and pg_get_constraintdef(oid) like '%push%';")" == "1" ]] \
  || fail "the channel vocabulary has no push value, so SHIP-139 would need a migration to add one"
ok "push is in the vocabulary, routed by nothing, and waiting on SHIP-139 rather than half-built"

unset notif_customer_id notif_customer_email notif_token notif_job notif_event_id notif_group
unset notif_row notif_want notif_body notif_before notif_after notif_sent_sql notif_sms_event
unset notif_marker_id
unset -f publish_outbox run_notifier
