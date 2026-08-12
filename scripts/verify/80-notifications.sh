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
# Three of the four events are inserted with psql, which is exactly what the contract says a
# domain does — write the row inside the transaction making the change — and is the only way to
# arrange a transaction that rolls back. The fourth is emitted by a real endpoint: this section
# registers its own customer, creates a draft and cancels it, and SHIP-57's guard writes
# job.status_changed inside the transaction that moves the job.
#
# # Everything here is scoped to this section, and that is the second time it has had to be
#
# **This section owns nothing that another section can see, and reads nothing another section
# left behind.** That is a correction, not a principle stated up front. The first version
# compared the topic against `select id from outbox where published_at is not null` — every job
# event the database had ever published — on the reasoning that the topic had just been recreated
# empty, so the two had to agree.
#
# **SHIP-68 broke it, and the direction of the breakage is worth reading.** That section runs the
# real worker to demonstrate job expiry, and the worker runs *every* registered task, so it drains
# the outbox and marks job events published before this section wipes the topic. Those rows are
# legitimately published and legitimately absent from the fresh topic, so the equality could never
# hold again. Nothing was lost, and the product was right — the assertion was too broad.
#
# It is also why it was intermittent before the merge: on a machine where `shipper.job` did not
# yet exist, SHIP-68's publisher passes failed against the missing topic and left every row
# claimable, so this section drained them itself and the broad query happened to agree.
#
# **This is the second time cross-section state has bitten this harness** — SHIP-47's rate-limit
# bucket was the first, where one section's failed sign-ins spent an allowance another section
# then found empty. The recipe both times is the same: **fence what you assert on**. Here the
# fence is `published_at > $outbox_fence`, taken after the topic is recreated and before anything
# can publish, so the comparison is over exactly what this section's worker produced.
#
# The next section to run the worker, or to read Kafka, has to do the same. Note in particular
# that this section *deletes* `shipper.job`, which is safe today only because no other section
# asserts on a topic it did not create.

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

# Recreated by the command that owns the topic set rather than by kafka-topics.sh.
#
# That is a change from how this section was written. It used to create the topic itself, with
# three partitions and a comment saying SHIP-135 would take it over; **SHIP-135 has**, so the topic
# now comes back exactly as a deployment would make it. Three partitions still, and for the reason
# that comment gave: the key is the aggregate, so one job's events must share a partition for Kafka
# to keep their order within it.
#
# The command is idempotent and 50-jobs.sh has already run it once. The SHIP-135 section at the
# foot of this file asserts the whole set, the partition count, and that a second run changes
# nothing.
pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-topics" ./cmd/topics
popd >/dev/null
KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:29092}" \
  "$WORKDIR/shipper-topics" >"$WORKDIR/topics-recreate.log" 2>&1 \
  || { cat "$WORKDIR/topics-recreate.log"; fail "could not recreate $outbox_topic"; }
ok "topic $outbox_topic recreated empty by the command that owns the topic set (SHIP-135)"

# The fence. Taken from the database's own clock, after the topic is empty and before anything
# can publish into it — no worker is running at this point, SHIP-68's having been waited on — so
# `published_at > $outbox_fence` names exactly the events this section's worker sends to the
# topic it just created. See the header for what happened without it.
outbox_fence="$("$PSQL" "$DATABASE_URL" -tAc "select clock_timestamp();")"

# --- an event a real endpoint wrote ----------------------------------------------------------

# Its own customer, its own job. Reusing 50-jobs.sh's token would make this section depend on
# that one having run, which is the coupling the header is about.
outbox_request() {
  curl -s -X "$1" -o "$6" -w '%{http_code}' \
    -H "$auth_header: Bearer $2" \
    -H "Idempotency-Key: $3" \
    -H 'Content-Type: application/json' \
    -d "$5" "http://localhost:$VERIFY_PORT$4"
}

status="$(post_json "verify-outbox-cust-$$" /v1/auth/register \
  "{\"email\":\"outbox-customer-$$@example.com\",\"phone\":\"04180$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/outbox-customer.json")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/outbox-customer.json"; fail "could not register the outbox customer: $status"; }
outbox_token="$(mint_token "$(json "$WORKDIR/outbox-customer.json" '["id"]')")"

status="$(outbox_request POST "$outbox_token" "verify-outbox-draft-$$" /v1/jobs '{}' \
  "$WORKDIR/outbox-draft.json")"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/outbox-draft.json"; fail "could not create a draft to cancel: $status"; }
emitted_job="$(json "$WORKDIR/outbox-draft.json" '["id"]')"

# A cancellation is a transition, so it goes through SHIP-57's guard, which emits the event
# inside the same transaction. Nothing in this section writes that row.
status="$(outbox_request POST "$outbox_token" "verify-outbox-cancel-$$" \
  "/v1/jobs/$emitted_job/cancel" '{"reason": "Demonstrating the outbox end to end."}' \
  "$WORKDIR/outbox-cancelled.json")"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/outbox-cancelled.json"; fail "cancelling the draft returned $status, want 200"; }

emitted_rows="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox where aggregate_id = '$emitted_job' and published_at is null;")"
[[ "$emitted_rows" == "1" ]] \
  || fail "cancelling the job left $emitted_rows unpublished outbox row(s), want 1"
ok "a real endpoint's transition wrote its event to the outbox, unpublished, in its own transaction"

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
#
# Four: the three fixtures and the cancellation the endpoint emitted. Counted on the two
# aggregates this section owns rather than on the outbox as a whole, because what else is in
# there is another section's business — the same fencing the comparison below needs.
published=0
for _ in $(seq 1 100); do
  published="$("$PSQL" "$DATABASE_URL" -tAc \
    "select count(*) from outbox
      where aggregate_id in ('$committed_job', '$emitted_job') and published_at is not null;")"
  if [[ "$published" == "4" ]]; then break; fi
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

[[ "$published" == "4" ]] \
  || { cat "$WORKDIR/worker.log"; fail "$published of 4 events were marked published"; }
ok "the worker drained the outbox and marked all four published"

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

# What the topic holds against what the database says this worker run published — in both
# directions, which is what makes it worth doing. A row marked published that never left is the
# one failure the ordering of publish and mark exists to prevent; a message on the topic the
# outbox does not claim would mean something published without recording it.
#
# **Fenced on `published_at > $outbox_fence`**, not on `published_at is not null`. The broad form
# was this section's original defect: SHIP-68 runs the real worker, which drains the outbox before
# this section wipes the topic, so rows legitimately published into a topic that no longer exists
# would be counted as missing. The header carries the full account.
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
  "select id from outbox where aggregate_type = 'job' and published_at > '$outbox_fence' order by id;" \
  | tr -d ' ' | sort | tr '\n' ' ')"
[[ "$consumed_sorted" == "$outbox_sorted" ]] \
  || fail "the topic holds [$consumed_sorted] and the outbox says this run published [$outbox_sorted]"
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

# The event no part of this section wrote: the cancellation went through SHIP-57's guard, the
# guard emitted it, and it came off the topic with its aggregate and its type intact. That is the
# whole path — endpoint, transaction, outbox, worker, broker — and it is the reason the section
# creates its own job rather than counting on whatever earlier sections happened to leave
# unpublished. Counting those was the first version, and it was one race away from asserting
# nothing: SHIP-68's own expiry event is published by its own worker run about half the time.
emitted_type="$(python3 -c '
import json, sys
for line in open(sys.argv[1]):
    if not line.strip():
        continue
    e = json.loads(line)
    if e["aggregate_id"] == sys.argv[2]:
        print(e["type"])
        break
' "$WORKDIR/outbox-consumed.json" "$emitted_job")"
[[ "$emitted_type" == "job.status_changed" ]] \
  || fail "the cancelled job's event came off the topic as [$emitted_type], want job.status_changed"
ok "the event a real endpoint emitted through the status guard published alongside them"

# Looked up by id, not read off the first line. The first version read line one, which passed
# only because the fixtures happened to be first that run: with three partitions the consumer
# interleaves three streams, so "the first message" is not a thing this section gets to assume —
# the same mistake as the ordered comparison above, in a smaller place. The second run found it.
consumed_payload="$(python3 -c '
import json, sys
for line in open(sys.argv[1]):
    if not line.strip():
        continue
    e = json.loads(line)
    if e["id"] == sys.argv[2]:
        print("%s/%s" % (e["type"], e["payload"]["sequence"]))
        break
' "$WORKDIR/outbox-consumed.json" "${event_ids[0]}")"
[[ "$consumed_payload" == "job.published/1" ]] \
  || fail "the first fixture came off the topic as [$consumed_payload], want job.published/1"
ok "each message carries its event id, its type and the payload the domain wrote"

# ---------------------------------------------------------------------------------------
ticket "SHIP-135  topics exist with a versioned schema for each domain event"

# The half of SHIP-134 that ticket deliberately left open, and both of Docs/11 §9's questions
# behind it: what creates the topics, and what the outbox does with an event that can never be
# published.
#
# # What is demonstrated here and what is demonstrated by tests
#
# The catalogue itself — which events exist, what their payloads contain, what a version is, and
# every refusal that keeps a permanently unpublishable row out of the outbox — is held by
# internal/events/catalogue_test.go and by cmd/api/events_golden.txt. None of that needs a broker
# and none of it is repeated here.
#
# What only a real cluster can show is the other half: that the topic set the catalogue implies is
# actually on the broker with the partition count it asked for, that applying it twice is safe,
# that a topic which drifted is reported rather than silently corrected, and that the version
# reaches a consumer both in the envelope and in the payload. That is what this section does, and
# it runs last in the file because it reads what the SHIP-134 section above put on the topic.
#
# # Fencing
#
# It asserts on shipper.job only through $WORKDIR/outbox-consumed.json, which the section above
# produced from a topic it had just emptied. The one topic it manipulates is **shipper.delivery**,
# which no code publishes to yet — deliberately, so that breaking a topic on purpose cannot cost
# another section anything.

topics_apply() {
  KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:29092}" \
    "$WORKDIR/shipper-topics" >"$1" 2>&1
}

topic_described() {
  "${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" \
    --bootstrap-server localhost:9092 --describe --topic "$1" 2>/dev/null | tr -d '\r' | head -1
}

# --- the topic set exists ---------------------------------------------------------------------

# The three aggregates of internal/events.Aggregates. Written out rather than read from the
# binary, because a check that asked the code what to expect would agree with the code by
# construction — the Go test holds the set, and this holds the cluster to it.
retention_stated=1
for topic in shipper.job shipper.bid shipper.delivery; do
  described="$(topic_described "$topic")"
  [[ "$described" == *"PartitionCount: 3"* ]] \
    || fail "$topic is not on the broker with three partitions: ${described:-nothing at all}"
  [[ "$described" == *"retention.ms=604800000"* ]] || retention_stated=0
done
ok "shipper.job, shipper.bid and shipper.delivery all exist, three partitions each"

# Three is the number that cannot be taken back: partitions can be added and never removed, and
# adding one rehashes every key onto a different partition, which silently ends the per-aggregate
# ordering the section above just demonstrated.
[[ "$retention_stated" == "1" ]] \
  || fail "retention.ms is not stated on the topics, so the set is not reproducible on a broker whose default differs"
ok "retention is stated on each topic rather than inherited, so the set is reproducible"

# --- applying it again is safe ------------------------------------------------------------------

# The property that lets a deployment run it every time, and lets 50-jobs.sh run it too.
topics_apply "$WORKDIR/topics-again.log" \
  || { cat "$WORKDIR/topics-again.log"; fail "a second application of the topic set failed"; }
if grep -qE '^ +shipper\.[a-z]+ +created' "$WORKDIR/topics-again.log"; then
  cat "$WORKDIR/topics-again.log"
  fail "the second application created something, so it is not idempotent"
fi
[[ "$(grep -cE '^ +shipper\.[a-z]+ +exists$' "$WORKDIR/topics-again.log")" == "3" ]] \
  || { cat "$WORKDIR/topics-again.log"; fail "the second application did not find all three topics"; }
ok "applying the set again creates nothing and still exits zero — safe on every deploy"

# --- a topic that drifted is refused, not corrected ---------------------------------------------

# Staged on shipper.delivery, which nothing publishes to yet. This is the failure the command
# exists to catch: somebody creates a topic by hand to unblock something, with the default one
# partition, and every consumer's ordering guarantee quietly goes with it.
"${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" --bootstrap-server localhost:9092 \
  --delete --topic shipper.delivery >/dev/null 2>&1 || true
for _ in $(seq 1 50); do
  "${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" --bootstrap-server localhost:9092 \
    --list 2>/dev/null | tr -d '\r' | grep -qx shipper.delivery || break
  sleep 0.2
done
"${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" --bootstrap-server localhost:9092 \
  --create --topic shipper.delivery --partitions 1 --replication-factor 1 >/dev/null 2>&1 \
  || fail "could not stage a one-partition shipper.delivery"

if topics_apply "$WORKDIR/topics-drifted.log"; then
  cat "$WORKDIR/topics-drifted.log"
  fail "the command accepted shipper.delivery with one partition"
fi
grep -q "partitions" "$WORKDIR/topics-drifted.log" \
  || { cat "$WORKDIR/topics-drifted.log"; fail "it failed without saying the partition count was wrong"; }
[[ "$(topic_described shipper.delivery)" == *"PartitionCount: 1"* ]] \
  || fail "the refused run changed the topic anyway; adding a partition is a decision, not a repair"
ok "a topic somebody created by hand with one partition is reported and left alone, never repaired"

"${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" --bootstrap-server localhost:9092 \
  --delete --topic shipper.delivery >/dev/null 2>&1 || true
for _ in $(seq 1 50); do
  "${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" --bootstrap-server localhost:9092 \
    --list 2>/dev/null | tr -d '\r' | grep -qx shipper.delivery || break
  sleep 0.2
done
topics_apply "$WORKDIR/topics-restore.log" \
  || { cat "$WORKDIR/topics-restore.log"; fail "the topic set could not be applied again"; }
[[ "$(topic_described shipper.delivery)" == *"PartitionCount: 3"* ]] \
  || fail "shipper.delivery did not come back with three partitions"
ok "and once the wrong topic is gone the set applies cleanly again"

# --- the version travels with the message -------------------------------------------------------

# The cancellation the section above put through a real endpoint. It went through internal/events,
# so its row carries the version its schema had **when the row was written** — which is the whole
# reason the version is in the payload rather than looked up at publish time. A deployment that
# changes a payload while rows are still unpublished would otherwise relabel them as the new shape.
stored_version="$("$PSQL" "$DATABASE_URL" -tAc \
  "select payload->>'schema_version' from outbox where aggregate_id = '$emitted_job';")"
[[ "$stored_version" == "1" ]] \
  || fail "the stored payload reports schema version [$stored_version], want 1"
ok "the outbox row records the version it was written under, readable with one select"

wire_version="$(python3 -c '
import json, sys
for line in open(sys.argv[1]):
    if not line.strip():
        continue
    e = json.loads(line)
    if e["aggregate_id"] == sys.argv[2]:
        print("%s/%s/%s" % (e["type"], e.get("schema_version"), e["payload"].get("schema_version")))
        break
' "$WORKDIR/outbox-consumed.json" "$emitted_job")"
[[ "$wire_version" == "job.status_changed/1/1" ]] \
  || fail "the message came off the topic as [$wire_version], want job.status_changed/1/1"
ok "and it reaches a consumer in the envelope as well as inside the payload"

# Repeated as a Kafka header so a consumer can refuse a version it was not built for without
# deserialising the body at all — which is what makes checking it something anybody actually does.
#
# Asserted as two populations, and the split is the interesting part. The event a domain emitted
# through internal/events carries the header; the three this file wrote with psql, to arrange a
# transaction that rolls back, carry none. A hand-written row is published **with no version rather
# than with the catalogue's current one**, because the catalogue's answer today is a claim about a
# row that nothing knows to be true — which is the same reason the version is stamped when the row
# is written rather than looked up when it is published.
#
# # Fenced by event id, and this is the third instance of that lesson in this harness
#
# The first version of this check counted messages by event type across the whole topic, and it
# failed on a machine where another git worktree was running `make verify` at the same time. The
# database and the ports are per worktree; **`COMPOSE_PROJECT_NAME` is pinned so the stack is not**,
# so there is one broker and one `shipper.job` for every worktree on the machine. The other run's
# events — from a build without this ticket in it — were on the topic and carried no version, and
# the count was right about what it saw.
#
# So this reads only the ids it created. That is the recipe SHIP-47's rate-limit bucket produced
# and the SHIP-134 comparison above adopted, one layer further out: **fence what you assert on**,
# and note that on this topic the fence has to be an id rather than a timestamp, because a
# concurrent run is not ordered against this one.
emitted_event_id="$("$PSQL" "$DATABASE_URL" -tAc \
  "select id from outbox where aggregate_id = '$emitted_job';" | tr -d ' ')"

headers="$("${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-console-consumer.sh" \
  --bootstrap-server localhost:9092 --topic "$outbox_topic" --from-beginning \
  --property print.headers=true --max-messages 200 --timeout-ms 8000 2>/dev/null | tr -d '\r' || true)"

# `event-id:` appears only in the header portion, and the payload spells the field schema_version
# with an underscore, so neither pattern can match inside a message body.
grep -q "event-id:$emitted_event_id.*schema-version:1" <<<"$headers" \
  || { printf '%s\n' "$headers"; fail "the event the endpoint emitted is not headed schema-version:1"; }

for id in "${event_ids[@]}"; do
  header_line="$(grep "event-id:$id" <<<"$headers" || true)"
  [[ -n "$header_line" ]] || { printf '%s\n' "$headers"; fail "fixture $id is not on the topic"; }
  [[ "$header_line" != *"schema-version"* ]] \
    || fail "fixture $id was published claiming a schema version it was never written with"
done
ok "the version is a Kafka header on the event a domain emitted, and on none of the three written by hand"

# --- there is a schema for each domain event, and it is committed ---------------------------------

# cmd/api/events_golden.txt is the catalogue rendered: topic, type, version, payload bound and the
# payload's field set, one line per event, derived from the payload structs by reflection. A
# payload that changes shape moves a line; a line that moved without its `v` moving is the defect
# the file exists to make visible in review.
events_golden="$ROOT/services/core/cmd/api/events_golden.txt"
for event in job.status_changed job.expiry_warned job.expiry_extended; do
  grep -qE "^shipper\.job +$event +v[0-9]+ +[0-9]+ +[a-z_]+:" "$events_golden" \
    || fail "$event has no schema recorded in cmd/api/events_golden.txt"
done
ok "each of the three domain events has a recorded schema: topic, version, bound and field set"

# The budget-privacy invariant, applied to the catalogue rather than to one payload. An event is
# the worst place for it to appear, because it travels past every point a response body could have
# redacted it (Docs/01 §4.3).
if grep -qi "budget" "$events_golden"; then
  fail "an event in the catalogue declares a budget field"
fi
ok "and no event in the catalogue declares a budget field, in this domain or any later one"

unset consumed consumed_ids consumed_sorted outbox_sorted ours_in_order emitted_type consumed_payload
unset published worker_pid outbox_fence outbox_token emitted_job emitted_rows
unset committed_job rolled_back_job rolled_back_rows outbox_topic
unset event_ids
unset described retention_stated stored_version wire_version headers events_golden event topic
unset emitted_event_id header_line id
unset -f outbox_request topics_apply topic_described
