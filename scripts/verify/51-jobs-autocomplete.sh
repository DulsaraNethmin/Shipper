# shellcheck shell=bash
#
# SHIP-119 — the seventy-two hour auto-complete.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 50–59 is the jobs range, and this is a jobs sweep rather than a delivery one for a reason that is
# a reading of Docs/02 rather than a convenience: "a Delivered job with no dispute" is exactly "a
# job still in Delivered", because Disputed is a status in its own right and a dispute has already
# moved the job out. internal/jobs/autocomplete.go carries the argument.
#
# A file of its own rather than an addition to 50-jobs.sh, so that a track adds a file and edits
# none — the split scripts/verify-foundation.sh exists for.
#
# # What is demonstrated here and what is demonstrated by tests
#
# The claim's predicate, the transition, the actor, the two-worker race and the second pass that
# does nothing are all in cmd/worker/tasks_jobs_autocomplete_test.go, against a real PostgreSQL.
# What only this can show is the real binary doing it: the registration actually in the manifest,
# the sweep running on its own ticker inside a process that is also running four other tasks, and a
# job moving without anybody calling anything.
#
# # Owning what this asserts about, before the worker starts
#
# The harness header sets two rules for a section that runs cmd/worker, and the second is the one
# that costs: **know what your fixtures look like to every other registered task.** There are five
# now, and this file's three jobs are checked against all of them:
#
#   * `job-expiry` and `job-expiry-warning` claim `status = 'Open'`. Every job here is walked
#     through Open to Delivered before the worker starts, so none is Open when it does.
#   * `bid-expiry` claims bids. This section creates none, and at this point in the run neither has
#     any other — 61-bidding.sh has not run yet.
#   * `outbox-publisher` has no "not due" state and drains whatever is unpublished. That is
#     harmless here for the same reason it is harmless in 50-jobs.sh: this file publishes only job
#     events, 80-notifications.sh recreates `shipper.job` empty before it asserts anything on it,
#     and it fences its own comparison on `published_at`. Nothing here asserts on Kafka at all.
#   * `job-auto-complete` is this section's own, and is the only task in the binary that finds
#     anything due.
#
# Every assertion below is keyed to a job identifier this section created. Never a count over a
# table: another worktree is running against the same broker and, if it is the primary tree, could
# be running against a database this one cannot see into but whose Kafka topics it shares.
#
# # The fixture backdates a history row, and that is the only way to reach this state
#
# `job_status_history` is append-only by trigger (000401), so the Delivered transition cannot be
# aged after the fact the way 50-jobs.sh's `age_job` ages a deadline with an UPDATE. It is written
# with its `server_recorded_at` already three days old instead — the one column 000401 says the
# guard never names, named here because a fixture that waited seventy-two hours is not a fixture.
# Everything else about the row is what the guard would have written.

ticket "SHIP-119  a Delivered job with no dispute becomes Completed after 72 hours"

# --- a customer and three jobs ------------------------------------------------------------------

# 04134 — the jobs range is 0413x and 50-jobs.sh uses 04130 to 04133.
status="$(post_json "verify-ac-cust-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"autocomplete-customer-$$@example.com\",\"phone\":\"04134$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/ac-customer.json")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/ac-customer.json"; fail "could not register the auto-complete customer: $status"; }
ac_customer_id="$(json "$WORKDIR/ac-customer.json" '["id"]')"
ac_token="$(mint_token "$ac_customer_id")"

ac_provider="$(uuidgen | tr 'A-Z' 'a-z')"

# ac_move <job> <from> <to> [age] — one transition through 000402's guard, optionally backdated.
#
# The same shape as 50-jobs.sh's move_job: a history row, the transaction-local setting that names
# it, and the UPDATE. The guard refuses the UPDATE without all three, so this is the guard being
# used rather than worked around.
#
# The fourth argument is an interval subtracted from `server_recorded_at`, and it is only ever
# passed for the move into Delivered. See the header.
ac_move() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 \
    -v job="$1" -v from_status="$2" -v to_status="$3" -v age="${4:-0 seconds}" \
    -v actor="$ac_provider" \
    >/dev/null <<'SQL'
BEGIN;
INSERT INTO job_status_history
    (id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at, server_recorded_at)
VALUES (gen_random_uuid(), :'job', :'from_status', :'to_status', 'provider', :'actor',
        now() - :'age'::interval, now() - :'age'::interval);
SELECT set_config('shipper.job_status_transition',
                  (SELECT id::text FROM job_status_history
                    WHERE job_id = :'job' AND to_status = :'to_status'
                    ORDER BY server_recorded_at DESC LIMIT 1), true);
UPDATE jobs SET status = :'to_status' WHERE id = :'job';
COMMIT;
SQL
}

# ac_deliver <name> <age> — a job walked from Draft to Delivered, delivered <age> ago.
#
# Through every status Docs/02 §2 puts between the two, rather than jumping: the guard would accept
# a jump, since it deliberately does not duplicate the transition table (000402 says so), and a
# fixture that could not happen in the product proves nothing about one that can.
ac_deliver() {
  local out="$WORKDIR/ac-job-$1.json" id
  [[ "$(curl -s -X POST -o "$out" -w '%{http_code}' \
        -H "$auth_header: Bearer $ac_token" -H "Idempotency-Key: verify-ac-$1-$$" \
        -H 'Content-Type: application/json' \
        -d "{\"goods_description\": \"Two pallets of ceramic tiles for $1\"}" \
        "http://localhost:$VERIFY_PORT/v1/jobs")" == "201" ]] \
    || { cat "$out"; fail "could not create the job for $1"; }
  id="$(json "$out" '["id"]')"

  ac_move "$id" Draft Open
  ac_move "$id" Open Awarded
  ac_move "$id" Awarded "Driver assigned"
  ac_move "$id" "Driver assigned" "En route to pickup"
  ac_move "$id" "En route to pickup" "Picked up"
  ac_move "$id" "Picked up" "In transit"
  ac_move "$id" "In transit" Delivered "$2"
  printf '%s' "$id"
}

# Three days and one hour ago: past the window with an hour to spare, so the assertion does not
# depend on how long the rest of this run takes.
ac_due="$(ac_deliver due '73 hours')"

# An hour ago: well inside the window. The customer has had one hour of their seventy-two.
ac_fresh="$(ac_deliver fresh '1 hour')"

# Delivered three days ago and then disputed, which is the half of the *Done when* that says "with
# no dispute". Docs/02 §3: a dispute freezes automatic completion until an administrator resolves
# it.
ac_disputed="$(ac_deliver disputed '73 hours')"
ac_move "$ac_disputed" Delivered Disputed

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select string_agg(status, ',' order by id) from jobs
    where id in ('$ac_due', '$ac_fresh', '$ac_disputed');")" == "Delivered,Delivered,Disputed" ]] \
  || fail "the fixture is not two Delivered jobs and one Disputed: $("$PSQL" "$DATABASE_URL" -tAc "select id, status from jobs where id in ('$ac_due', '$ac_fresh', '$ac_disputed');")"
ok "two deliveries and one dispute, each walked through every status Docs/02 §2 puts in the way"

# The deadline is derived rather than stored, which is what Docs/09's row means by "needs no column
# that does not already exist". 000401 recorded when the job entered Delivered and that is the only
# statement of it.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from information_schema.columns
    where table_name = 'jobs' and column_name = 'delivered_at';")" == "0" ]] \
  || fail "jobs has a delivered_at column; the transition record is already the answer and a second copy of it can drift"
ok "the seventy-two hours is counted from job_status_history, with no delivered_at column to disagree with it"

# --- the real worker, running every registered task ---------------------------------------------

pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-worker-ac" ./cmd/worker
popd >/dev/null
ok "the worker builds with the auto-complete task registered"

SHIPPER_ENV=development \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
  "$WORKDIR/shipper-worker-ac" >"$WORKDIR/worker-autocomplete.log" 2>&1 &
ac_worker_pid=$!

for _ in $(seq 1 100); do
  ac_status="$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$ac_due';")"
  [[ "$ac_status" == "Completed" ]] && break
  sleep 0.2
done

kill -TERM "$ac_worker_pid" 2>/dev/null || true
for _ in $(seq 1 50); do kill -0 "$ac_worker_pid" 2>/dev/null || break; sleep 0.2; done
wait "$ac_worker_pid" 2>/dev/null || true

[[ "$ac_status" == "Completed" ]] \
  || { cat "$WORKDIR/worker-autocomplete.log"; fail "the delivery seventy-three hours old is $ac_status, want Completed"; }
ok "a Delivered job with no dispute becomes Completed once seventy-two hours have passed"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$ac_fresh';")" == "Delivered" ]] \
  || fail "a delivery one hour old was completed; the customer had not had their seventy-two hours"
ok "and a delivery inside its window is left alone"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$ac_disputed';")" == "Disputed" ]] \
  || fail "a disputed delivery was auto-completed; Docs/02 §3 freezes completion until an administrator resolves it"
ok "a dispute stops the clock — the sweep asks about status, and a disputed job has left Delivered"

# --- what the record says -----------------------------------------------------------------------

# 000401's ck_job_status_history_actor_id predicted this ticket by name: the expiry sweep and the
# seventy-two hour auto-complete "both act with no user behind them, and both must still be
# attributable — to the platform, which is what 'system' says".
ac_recorded="$("$PSQL" "$DATABASE_URL" -tAc \
  "select h.from_status || '->' || h.to_status || ' ' || h.actor_type || ' ' || (h.actor_id is null)
     from job_status_history h
    where h.job_id = '$ac_due' and h.to_status = 'Completed';")"
[[ "$ac_recorded" == "Delivered->Completed system true" ]] \
  || fail "the recorded auto-completion is '$ac_recorded', want 'Delivered->Completed system true'"
ok "the transition is recorded against the platform, with no account behind it"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select reason from job_status_history where job_id = '$ac_due' and to_status = 'Completed';")" \
  == *"72 hours"* ]] \
  || fail "the auto-completion records no reason a customer or support could read"
ok "with a reason that says why, for support and for the customer's timeline"

# The event, which is what makes this reach a person at all — SHIP-137's consumer routes
# job.status_changed to Completed to both parties, and this is the row it will read.
ac_event="$("$PSQL" "$DATABASE_URL" -tAc \
  "select payload::text from outbox
    where aggregate_id = '$ac_due' and event_type = 'job.status_changed'
      and payload->>'to' = 'Completed';")"
[[ -n "$ac_event" ]] \
  || fail "the auto-completion emitted no domain event, so nothing downstream would ever hear about it"
[[ "$ac_event" == *'"from": "Delivered"'* || "$ac_event" == *'"from":"Delivered"'* ]] \
  || fail "the event does not say what the job moved from: $ac_event"
case "$ac_event" in
  *budget*) fail "the auto-completion event carries a budget (Docs/01 §4.3): $ac_event" ;;
esac
ok "and emits job.status_changed inside the same transaction, carrying no budget"

# --- a second pass, which is what the ticker does a quarter of an hour later ---------------------

SHIPPER_ENV=development \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
  "$WORKDIR/shipper-worker-ac" >"$WORKDIR/worker-autocomplete-again.log" 2>&1 &
ac_worker_pid=$!
sleep 2
kill -TERM "$ac_worker_pid" 2>/dev/null || true
for _ in $(seq 1 50); do kill -0 "$ac_worker_pid" 2>/dev/null || break; sleep 0.2; done
wait "$ac_worker_pid" 2>/dev/null || true

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$ac_due' and to_status = 'Completed';")" == "1" ]] \
  || { cat "$WORKDIR/worker-autocomplete-again.log"; fail "a second pass completed the same job again"; }
ok "a second pass completes nobody again — a Completed job is no longer Delivered, so the claim skips it"

unset ac_customer_id ac_token ac_provider ac_due ac_fresh ac_disputed ac_status ac_worker_pid
unset ac_recorded ac_event
unset -f ac_move ac_deliver
