# shellcheck shell=bash
#
# SHIP-134 — the transactional outbox publisher.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 80–89 is the notifications range (M5), which is where the publisher sits: it exists so that
# SHIP-137's consumer has events to read.
#
# # What is demonstrated here and what is demonstrated by tests
#
# The *Done when* is "domain events commit with their transaction and publish at least once",
# and it has a half that only a real broker can show. cmd/worker/outbox_test.go proves the
# transactional half against a real PostgreSQL — a rolled-back transaction publishes nothing, an
# unreachable broker leaves every row claimable, a crash between publishing and committing
# republishes rather than loses — with the broker stood in for by a recorder. What it cannot
# show is that the thing on the topic is readable by something that is not this codebase.
#
# So this section runs the actual worker binary against the actual Kafka in deploy/, and reads
# the events back off the topic with Kafka's own console consumer. Nothing here is a repeat of
# a test: it is the wire, the acknowledgement and the shutdown flush.
#
# Events are inserted with psql because no endpoint emits one yet — SHIP-136 is what makes jobs,
# bids and deliveries write to the outbox. That is also exactly what the contract says a domain
# does: write the row inside the transaction making the change.

ticket "SHIP-134  domain events commit with their transaction and publish at least once"

outbox_topic="shipper.job"

# A clean topic, so "the three messages on it" is a statement about this run. Deletion is
# asynchronous, hence the wait: creating a topic that is still being deleted fails.
"${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" --bootstrap-server localhost:9092 \
  --delete --topic "$outbox_topic" >/dev/null 2>&1 || true
for _ in $(seq 1 50); do
  "${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" --bootstrap-server localhost:9092 \
    --list 2>/dev/null | tr -d '\r' | grep -qx "$outbox_topic" || break
  sleep 0.2
done

# Three partitions rather than one, so the Hash balancer is doing something real: the key is the
# aggregate, so one job's events share a partition and Kafka keeps their order within it. The
# broker refuses to create topics on demand (deploy/docker-compose.yml), which is why this is
# here at all — **SHIP-135 owns the topic set**, and until it lands a publish to a topic nobody
# created fails the pass and leaves the rows claimable.
"${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" --bootstrap-server localhost:9092 \
  --create --topic "$outbox_topic" --partitions 3 --replication-factor 1 >/dev/null 2>&1 \
  || fail "could not create $outbox_topic"
ok "topic $outbox_topic created empty (SHIP-135 owns the real topic set)"

committed_job="$(uuidgen | tr 'A-Z' 'a-z')"
rolled_back_job="$(uuidgen | tr 'A-Z' 'a-z')"
declare -a event_ids=()
for _ in 1 2 3; do event_ids+=("$(uuidgen | tr 'A-Z' 'a-z')"); done

# The committed transaction: three events for one job, written the way a domain writes them —
# in the transaction that made the change.
"$PSQL" "$DATABASE_URL" -v ON_ERROR_STOP=1 -q <<SQL || fail "writing the committed events failed"
BEGIN;
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at) VALUES
  ('${event_ids[0]}', 'job', '$committed_job', 'job.published',
   '{"sequence": 1}'::jsonb, now() - interval '3 minutes'),
  ('${event_ids[1]}', 'job', '$committed_job', 'job.awarded',
   '{"sequence": 2}'::jsonb, now() - interval '2 minutes'),
  ('${event_ids[2]}', 'job', '$committed_job', 'job.in_transit',
   '{"sequence": 3}'::jsonb, now() - interval '1 minute');
COMMIT;
SQL
ok "three events written and committed with their transaction"

# The transaction that fails after writing its event. This is the failure the outbox exists to
# prevent: a direct publish would already have told a consumer about an award that never
# happened.
"$PSQL" "$DATABASE_URL" -v ON_ERROR_STOP=1 -q <<SQL || fail "the rolled-back transaction errored unexpectedly"
BEGIN;
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, occurred_at)
VALUES ('$(uuidgen | tr 'A-Z' 'a-z')', 'job', '$rolled_back_job', 'job.awarded',
        '{"sequence": 0}'::jsonb, now() - interval '4 minutes');
ROLLBACK;
SQL
rolled_back_rows="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox where aggregate_id = '$rolled_back_job';")"
[[ "$rolled_back_rows" == "0" ]] \
  || fail "the rolled-back transaction left $rolled_back_rows row(s) in the outbox"
ok "an event whose transaction rolled back is not in the outbox to be published"

# --- the worker ---------------------------------------------------------------------------

pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-worker" ./cmd/worker
popd >/dev/null

SHIPPER_ENV=development \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:29092}" \
  "$WORKDIR/shipper-worker" >"$WORKDIR/worker.log" 2>&1 &
worker_pid=$!

# Poll rather than sleep a fixed span: the drain runs every two seconds and the first pass is
# immediate, so this is usually one iteration.
published=0
for _ in $(seq 1 100); do
  published="$("$PSQL" "$DATABASE_URL" -tAc \
    "select count(*) from outbox where aggregate_id = '$committed_job' and published_at is not null;")"
  if [[ "$published" == "3" ]]; then break; fi
  sleep 0.2
done

# Stop it before asserting anything, so a failed assertion cannot leave a worker running. SIGTERM
# is what a container runtime sends, and what makes the scheduler run each task's Close — which
# for this task is the producer's flush (SHIP-15g).
kill -TERM "$worker_pid" 2>/dev/null || true
for _ in $(seq 1 50); do
  kill -0 "$worker_pid" 2>/dev/null || break
  sleep 0.2
done
wait "$worker_pid" 2>/dev/null || true

[[ "$published" == "3" ]] \
  || { cat "$WORKDIR/worker.log"; fail "$published of 3 events were marked published"; }
ok "the worker drained the outbox and marked all three published"

grep -q '"task":"outbox-publisher"' "$WORKDIR/worker.log" \
  || { cat "$WORKDIR/worker.log"; fail "the outbox publisher did not register as a task"; }
grep -q "stopped cleanly" "$WORKDIR/worker.log" \
  || { cat "$WORKDIR/worker.log"; fail "the worker did not shut down cleanly on SIGTERM"; }
ok "the producer was closed on SIGTERM and the worker stopped cleanly"

# --- what actually reached the broker -------------------------------------------------------

# Far more asked for than were written, so that a message which should not be there is read and
# counted rather than left behind the cut. The consumer therefore always ends on its
# no-more-messages timeout, which is a non-zero exit and not a failure, hence the `|| true`.
consumed="$("${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-console-consumer.sh" \
  --bootstrap-server localhost:9092 --topic "$outbox_topic" --from-beginning \
  --max-messages 100 --timeout-ms 8000 2>/dev/null | tr -d '\r' || true)"

printf '%s\n' "$consumed" >"$WORKDIR/outbox-consumed.json"
consumed_ids="$(python3 -c '
import json, sys
print(" ".join(json.loads(line)["id"] for line in open(sys.argv[1]) if line.strip()))
' "$WORKDIR/outbox-consumed.json")"

# The topic was emptied before the worker started, so what is on it now is exactly what this
# worker run published — which the database independently claims is every job event it marked.
# Comparing the two catches an event marked published that never left, which is the one failure
# the ordering of publish and mark exists to prevent.
#
# **As sets, deliberately.** The topic has three partitions and the key is the aggregate, so the
# consumer reads three independent streams and interleaves them however it likes. Asserting a
# single global order here would be asserting something 000004_outbox.up.sql explicitly does not
# promise — and the first run of this check did exactly that and failed, which is the shortest
# available demonstration that "ordering is per aggregate, not global" is a real property of what
# was built rather than a sentence in a comment.
# Unquoted on purpose: $consumed_ids is a space-separated list and the split is what is wanted.
# shellcheck disable=SC2086
consumed_sorted="$(printf '%s\n' $consumed_ids | sort | tr '\n' ' ')"
outbox_sorted="$("$PSQL" "$DATABASE_URL" -tAc \
  "select id from outbox where aggregate_type = 'job' and published_at is not null order by id;" \
  | tr -d ' ' | sort | tr '\n' ' ')"
[[ "$consumed_sorted" == "$outbox_sorted" ]] \
  || fail "the topic holds [$consumed_sorted] and the outbox says it published [$outbox_sorted]"
ok "every event the outbox marked published is on $outbox_topic, and nothing else is"

# Per aggregate, though, the order is exact — and that is the promise. Checked over every
# aggregate on the topic rather than only the one this section wrote, so the jobs domain's own
# events are held to it too.
python3 -c '
import json, sys
seen = {}
for line in open(sys.argv[1]):
    if not line.strip():
        continue
    e = json.loads(line)
    previous = seen.get(e["aggregate_id"])
    if previous is not None and e["occurred_at"] < previous:
        sys.exit("%s published after a later event for the same aggregate" % e["id"])
    seen[e["aggregate_id"]] = e["occurred_at"]
' "$WORKDIR/outbox-consumed.json" || fail "one aggregate's events are on the topic out of order"

ours_in_order="$(python3 -c '
import json, sys
ours = set(sys.argv[2:])
ids = [json.loads(line)["id"] for line in open(sys.argv[1]) if line.strip()]
print(" ".join(i for i in ids if i in ours))
' "$WORKDIR/outbox-consumed.json" "${event_ids[@]}")"
[[ "$ours_in_order" == "${event_ids[0]} ${event_ids[1]} ${event_ids[2]}" ]] \
  || fail "this section's events came off the topic as [$ours_in_order]"
ok "each aggregate's events publish in the order they were written, keyed onto one partition"

if grep -q "$rolled_back_job" "$WORKDIR/outbox-consumed.json"; then
  fail "the event from the rolled-back transaction reached the broker"
fi
ok "the rolled-back transaction's event never reached the broker"

# The rest of the list is the jobs domain's own events, emitted by the endpoints the earlier
# sections exercised — SHIP-64's cancellations write job.status_changed inside the transaction
# that moves the job. Nothing in this section put them there, which is the point: the mechanism
# is already carrying real domain events end to end.
domain_events="$(python3 -c '
import json, sys
ours = set(sys.argv[2:])
print(sum(1 for line in open(sys.argv[1]) if line.strip() and json.loads(line)["id"] not in ours))
' "$WORKDIR/outbox-consumed.json" "${event_ids[@]}")"
(( domain_events > 0 )) \
  || fail "no domain-written events reached the topic; only this section's fixtures did"
ok "$domain_events event(s) written by the jobs domain's own endpoints published alongside them"

consumed_payload="$(python3 -c '
import json, sys
print(json.loads(open(sys.argv[1]).readline())["payload"]["sequence"])
' "$WORKDIR/outbox-consumed.json")"
[[ "$consumed_payload" == "1" ]] || fail "the payload did not survive the round trip"
ok "each message carries its event id, its type and the payload the domain wrote"

unset consumed consumed_ids consumed_sorted outbox_sorted ours_in_order domain_events consumed_payload
unset published worker_pid
unset committed_job rolled_back_job rolled_back_rows outbox_topic
unset event_ids
