# shellcheck shell=bash
#
# SHIP-171b — a pseudonymised account stops being a notification recipient.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# # Why this is a file of its own, and why it is numbered 82
#
# The harness's own header states the mechanism: "a track adds a file here and edits none".
# 80-notifications.sh is the outbox and the topics (SHIP-134 … SHIP-136) and 81-notifier.sh is the
# consumer and the dispatcher (SHIP-137 … SHIP-142); this is a defect in what the consumer and the
# dispatcher do about an account another domain erased, which is neither. 82 puts it inside the
# notifications range and **after 81**, which is load-bearing rather than tidy — see below.
#
# # It runs on 40-identity.sh's subject, and brings its own machinery
#
# Sections are sourced into one shell, so what an earlier one set is in scope — but **81-notifier.sh
# unsets every helper and variable it defined** when it finishes, which is a hygiene convention this
# file discovered by depending on it and failing. So the consumer is built and driven here, with
# helpers of this section's own.
#
# **40-identity.sh's `$pseudo_user` is the one thing taken from elsewhere, and it is worth the
# coupling.** It is a real account, registered through the API, whose deletion request was made
# through the endpoint and executed by the real worker — so its `notifications` rows carry the
# pseudonym because SHIP-171's own adapter put it there rather than because this file typed it. It
# also carries a **second, still-open** request, added by that section to prove a completed request
# does not stop a later one, which is exactly the row a lookup written as `state <> 'requested'`
# would get wrong. A fixture written here with psql would assert the same words about a weaker
# claim. It is checked for rather than assumed: if 40-identity.sh ever adopts 81's unset discipline
# this section fails loudly and says what to do, which is the right failure.
#
# The consumer group is **81-notifier.sh's own name, composed rather than inherited**. A fresh group
# starts at the beginning of `shipper.bid`, which is deliberately padded to eighteen hundred
# messages; the caught-up group makes this section seconds rather than minutes. Composing the string
# rather than reading the variable is what survives that section's `unset`, and if the name ever
# changes this section gets a new group and is merely slower.

ticket "SHIP-171b  a pseudonymised account stops being a notification recipient"

[[ -n "${pseudo_user:-}" ]] \
  || fail "40-identity.sh left no pseudonymised account in \$pseudo_user; if it now unsets its own variables, this section needs a fixture of its own"

pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-notifier-171b" ./cmd/notifier
go build -o "$WORKDIR/shipper-worker-171b" ./cmd/worker
popd >/dev/null

deletion_group="shipper-notifications-verify-$$"

# publish_171b <logfile> <outbox-id> — one short worker run, which drains whatever is unpublished.
publish_171b() {
  local log="$1" waiting="$2" pid
  SHIPPER_ENV=development \
  LOG_FORMAT=json \
  LOG_LEVEL=info \
  DATABASE_URL="$DATABASE_URL" \
  REDIS_URL="$REDIS_URL" \
  KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:29092}" \
    "$WORKDIR/shipper-worker-171b" >"$log" 2>&1 &
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

# dispatch_171b <logfile> <predicate-sql> — run the consumer until the predicate holds, then stop.
#
# Stopped before anything is asserted, so a failed assertion cannot leave a consumer running and
# holding a group membership. 81-notifier.sh's shape and its two-minute budget, for its reasons.
dispatch_171b() {
  local log="$1" predicate="$2" pid
  SHIPPER_ENV=development \
  LOG_FORMAT=json \
  LOG_LEVEL=info \
  DATABASE_URL="$DATABASE_URL" \
  REDIS_URL="$REDIS_URL" \
  KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:29092}" \
    "$WORKDIR/shipper-notifier-171b" -group "$deletion_group" >"$log" 2>&1 &
  pid=$!
  for _ in $(seq 1 600); do
    [[ "$("$PSQL" "$DATABASE_URL" -tAc "$predicate")" == "t" ]] && break
    sleep 0.2
  done
  kill -TERM "$pid" 2>/dev/null || true
  for _ in $(seq 1 75); do kill -0 "$pid" 2>/dev/null || break; sleep 0.2; done
  wait "$pid" 2>/dev/null || true
}
ok "cmd/notifier and cmd/worker are built for this section, which drives them itself"

# --- the state that produced the defect, restated as facts rather than as a premise -------------

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from account_deletion_requests
    where user_id = '$pseudo_user' and state = 'completed';")" == "1" ]] \
  || fail "the subject has no completed deletion request, so nothing below is about a deleted account"

# The still-open second request. Its presence is what makes the check above a check: an
# implementation reading "does this account have a deletion request" rather than "has one been
# executed" would suppress a person who is inside the thirty days Docs/05 §3.1 promises them.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from account_deletion_requests
    where user_id = '$pseudo_user' and state <> 'completed';")" -ge 1 ]] \
  || fail "the subject has no second, open request — see 40-identity.sh, which records one"
ok "the subject is an account SHIP-171 executed, and it also holds a later open request"

# The rows themselves, and the copy that is the whole reason pseudonymising `users` did not fix
# this: `notifications.address` is resolved once, when the row is written (000700), and SHIP-171
# rewrote the copy rather than removing the row.
pseudo_notif_total="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where recipient_id = '$pseudo_user';")"
[[ "$pseudo_notif_total" -ge 3 ]] \
  || fail "the subject has $pseudo_notif_total notification row(s), want the three 40-identity.sh wrote"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications
    where recipient_id = '$pseudo_user' and channel in ('email', 'sms')
      and address = 'deleted:$pseudo_user';")" == "2" ]] \
  || fail "the subject's contact rows do not carry the pseudonym, so the fixture is not the one this is about"
ok "$pseudo_notif_total notification rows are queued to it, two of them addressed to the pseudonym itself"

# --- half two: nothing already queued dispatches to it ------------------------------------------

# Run the dispatcher until nothing of the subject's is claimable. 81-notifier.sh's own run will
# usually have done this already — its rows are older, and the claim takes the oldest first — and
# running it here anyway is what stops this section depending on that having happened.
dispatch_171b "$WORKDIR/notif-deleted.log" \
  "select count(*) = 0 from notifications
    where recipient_id = '$pseudo_user' and status not in ('sent', 'undeliverable');"

pseudo_claimable="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications
    where recipient_id = '$pseudo_user' and status not in ('sent', 'undeliverable');")"
[[ "$pseudo_claimable" == "0" ]] \
  || { cat "$WORKDIR/notif-deleted.log"; fail "$pseudo_claimable of the subject's rows are still claimable"; }

# **`sent` is the failure this looks for, not an alternative outcome.** Development wires the
# console email and SMS adapters, which accept anything; without the suppression every one of these
# rows is marked `sent` to an address that is not an address, and the platform's own record says a
# deleted person was successfully contacted.
pseudo_sent="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where recipient_id = '$pseudo_user' and status = 'sent';")"
[[ "$pseudo_sent" == "0" ]] \
  || fail "$pseudo_sent notification(s) were dispatched to an account the platform has deleted"
ok "not one of them was dispatched: nothing was sent to an account the platform has deleted"

# The counter, which is the clause the row is written around. `undeliverable` alone would be met by
# marking the row after attempting it; attempts staying at 0 is what says no attempt was made.
pseudo_retired="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications
    where recipient_id = '$pseudo_user' and status = 'undeliverable' and attempts = 0
      and sent_at is null and last_error is not null;")"
[[ "$pseudo_retired" == "$pseudo_notif_total" ]] \
  || fail "$pseudo_retired of $pseudo_notif_total rows are retired with attempts still 0"
ok "all $pseudo_notif_total are terminal with attempts still 0, a reason recorded and no sent_at"

# Every channel, including push. The address on a push row is a device token, which SHIP-171
# deliberately did not overwrite and SHIP-172 owns — so the row is retired and the token is left
# exactly where it was, because what was aimed at which handset stays answerable.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select address from notifications
    where recipient_id = '$pseudo_user' and channel = 'push';")" == "verify-device-token-$$" ]] \
  || fail "the push row's device token was altered; retiring a row must not rewrite what it held"
ok "the push row is retired too, and its device token is untouched"

# --- half one: nothing new is written to it -----------------------------------------------------

# Two controls, because an assertion whose content is an absence passes when the consumer is broken.
#
# 0418x is the notifications range and **this file takes 04185 and 04186, chosen by grep rather
# than by reading the list**. 81-notifier.sh's header says it takes "04181 and 04182"; it takes
# 04181 and **04183**, and 04182 is unused. Taking 04183 on that comment's word cost this section a
# `409 identity_phone_taken` on a full harness run — so the prefixes below were measured with
# `grep -rho '0418[0-9]\$\$' scripts/verify/*.sh`, which reports 04180, 04181 and 04183.
#
# The second control is inserted with psql rather than registered, because the address it needs is
# one the API refuses: `plausibleEmail` will not accept a pseudonym, which is SHIP-171's own doing
# and is the point of the control.
status="$(post_json "verify-171b-live-$$" /v1/auth/register \
  "{\"name\":\"Verify Live Provider\",\"email\":\"live-provider-$$@example.com\",\"phone\":\"04185$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/live-provider.json")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/live-provider.json"; fail "could not register the live control provider: $status"; }
live_provider="$(json "$WORKDIR/live-provider.json" '["id"]')"

# **The control that rules out a string test.** This account has never asked to be deleted and its
# stored address reads exactly like a pseudonym. An implementation refusing anything that looks
# deleted would silently drop its notifications; the suppression is keyed on the account having been
# erased, so it must not.
lookalike_provider="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into users (id, email, phone, password_hash, role)
   values (gen_random_uuid(), 'deleted:' || gen_random_uuid(), '04186$$', 'x', 'provider')
   returning id;" | tr -d ' ')"
[[ -n "$lookalike_provider" ]] || fail "could not insert the pseudonym-shaped control account"
ok "two controls exist: a live provider, and one whose address reads like a pseudonym but is not deleted"

# One event per account, on `shipper.bid`. `bid.rejected`'s only audience is the provider the
# payload names, so this resolves without a job or a bid row and without the Parties port — the
# narrowest event that reaches exactly the account under test.
#
# **`offered_by` is `customer` on purpose.** notifications/consume.go suppresses the bid provider
# when the payload says the offer was theirs, so `provider` would resolve to nobody for all three
# and every assertion below would pass on a broken consumer. (That suppression also means a losing
# provider is never told their own offer was rejected, which is a defect this section discovered and
# does not fix — Docs/11 §3 records it.)
deletion_events=""
for target in "$pseudo_user" "$live_provider" "$lookalike_provider"; do
  event_id="$("$PSQL" "$DATABASE_URL" -tAqc "
    INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at)
    VALUES (gen_random_uuid(), 'bid', gen_random_uuid(), 'bid.rejected',
            jsonb_build_object(
                'schema_version', 1,
                'bid_id', gen_random_uuid()::text,
                'job_id', gen_random_uuid()::text,
                'provider_id', '$target',
                'offered_by', 'customer',
                'status', 'Rejected'),
            now())
    RETURNING id;" | tr -d ' ' | head -1)"
  [[ -n "$event_id" ]] || fail "could not write the event for $target"
  deletion_events="$deletion_events $event_id"
  case "$target" in
    "$pseudo_user")         deleted_event="$event_id" ;;
    "$live_provider")       live_event="$event_id" ;;
    "$lookalike_provider")  lookalike_event="$event_id" ;;
  esac
done
ok "three bid.rejected events are in the outbox, one per account"

publish_171b "$WORKDIR/notif-deleted-publish.log" "$deleted_event"
for event_id in $deletion_events; do
  [[ "$("$PSQL" "$DATABASE_URL" -tAc \
    "select published_at is not null from outbox where id = '$event_id';")" == "t" ]] \
    || { cat "$WORKDIR/notif-deleted-publish.log"; fail "$event_id was never published"; }
done
ok "and cmd/worker published all three onto shipper.bid"

# Waited on the **controls** rather than on the account under test, which is the only fence that
# works here: the claim being made about the deleted account is that nothing appears, and a wait for
# nothing to appear is satisfied before the consumer has read anything at all.
#
# `status = 'sent'` rather than the row merely existing, so the wait covers a consume *and* a
# dispatch — which is what makes the pass a genuine second look at the retired rows below.
dispatch_171b "$WORKDIR/notif-deleted-consume.log" \
  "select count(*) = 2 from notifications
    where event_id in ('$live_event', '$lookalike_event') and status = 'sent';"

live_rows="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where event_id = '$live_event';")"
[[ "$live_rows" -ge 1 ]] \
  || { cat "$WORKDIR/notif-deleted-consume.log"; fail "the live control got no notification, so the consumer is not working"; }

lookalike_rows="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where event_id = '$lookalike_event';")"
[[ "$lookalike_rows" -ge 1 ]] \
  || fail "the pseudonym-shaped control got no notification — the suppression is reading the address, not the account"
ok "both controls were notified, including the one whose address reads like a pseudonym"

deleted_rows="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications where event_id = '$deleted_event';")"
[[ "$deleted_rows" == "0" ]] \
  || fail "$deleted_rows notification(s) were written for an account the platform has deleted"
ok "and no row at all was written for the deleted account: it is no longer a recipient"

# Nothing left claimable that this section created, which is the harness's own rule for a section
# that starts a worker or a consumer.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications
    where event_id in ('$live_event', '$lookalike_event')
      and status not in ('sent', 'undeliverable');")" == "0" ]] \
  || fail "this section left notifications due that it is not demonstrating"
ok "both controls' notifications were dispatched, so nothing is left due"

# --- and the counter has stopped rather than paused ---------------------------------------------

# The run above is a **second** dispatcher pass over the subject's rows, and the controls reaching
# `sent` is the evidence that it happened rather than an assumption that it did. That distinction is
# the whole check: a row left `pending` or `failed` would also read attempts=0 after one pass, and
# would be claimed again here, which is the defect rather than the fix.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from notifications
    where recipient_id = '$pseudo_user' and (attempts <> 0 or status <> 'undeliverable');")" == "0" ]] \
  || fail "a later dispatcher pass moved the subject's rows, so the counter has not stopped"
ok "and a later pass leaves every one of them exactly as it was: the counter has stopped"

unset pseudo_notif_total pseudo_claimable pseudo_sent pseudo_retired deletion_group
unset live_provider lookalike_provider deletion_events deleted_event live_event lookalike_event
unset live_rows lookalike_rows deleted_rows event_id target
unset -f publish_171b dispatch_171b
