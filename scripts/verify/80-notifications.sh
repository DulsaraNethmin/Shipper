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
# fence is `published_at > $outbox_fence`, taken before anything can publish, so the comparison is
# over exactly what this section's worker produced.
#
# # Scoping to a section was never enough, and wave 10 is where that came due
#
# The paragraph above ended, for six waves, with a note saying this section *deletes* `shipper.job`
# and that doing so "is safe today only because no other section asserts on a topic it did not
# create". **That justification is scoped to sections and does not survive a second worktree.**
# `COMPOSE_PROJECT_NAME` is pinned, so five trees share one broker and one `shipper.job`, and wave 9
# proved by id rather than by timing that both directions break:
#
#   1. **Another tree's delete removes your messages.** A subset check fails, and an offset fence
#      does not help — the offsets it captured no longer exist. This file was the *cause* of that
#      failure mode for every other tree on the machine.
#   2. **Another tree's publish adds messages you did not expect.** An equality check fails, and
#      **no fence can fix it**: fencing narrows where you start reading, not what else arrives.
#
# # What was done about it, and what was deliberately not done
#
# **The equality was not weakened into a subset check.** That is how a guard quietly stops guarding,
# and wave 9 forbade it rather than doing it. What changed is *what the equality is over*.
#
# The old check drew its authority from the topic being empty: everything on it had to be this
# run's, because this run had just wiped it. Emptiness is not a property one worktree can establish
# about a shared topic — but **the database is genuinely per worktree**, so provenance can come from
# there instead. Every message read is one of exactly two things, and the outbox can tell them
# apart:
#
#   * an id this database's `outbox` holds — then it is ours, and it must be one this run
#     published. Asserted as an **equality**, not a subset;
#   * an id this database has never heard of — then it is another worktree's, and it is counted and
#     named rather than failed.
#
# That is strictly stronger than what it replaces in the direction that matters. The reverse half of
# the old equality existed to catch "a message on the topic the outbox does not claim", which is a
# publish that was never recorded; that case is still caught, because such a row *is* in this
# database's outbox and *is not* in the published set. What it no longer does is fail on a message
# that was never this tree's business.
#
# **And this section no longer deletes `shipper.job`.** It takes an offset fence instead, which is
# what the SHIP-136 section below already does for `shipper.bid` and `shipper.delivery`. That
# removes this file as a cause of failure mode 1 for every other tree; it cannot remove failure
# mode 1 as a possibility, because another tree running an older revision of this file still
# deletes. `kafka_consume_fenced` survives that — a fence beyond the end reads the whole partition —
# and the provenance split then keeps the assertion meaningful. **The residual is named rather than
# papered over: a delete landing between this run's publish and this run's read still loses
# messages, and only a per-tree topic prefix would close it. That is a change to the topic set and
# therefore not this section's to make.**
#
# The next section to run the worker, or to read Kafka, inherits the recipe: fence on an id, take
# provenance from the database, and never assert that a shared topic holds nothing else.

ticket "SHIP-134  domain events commit with their transaction and publish at least once"

outbox_topic="shipper.job"

# The topic set, applied rather than the topic deleted.
#
# **This section used to wipe `shipper.job` here**, so that "the three messages on it" was a
# statement about this run. Five worktrees share one broker, so that statement was never this
# tree's to make and the wipe destroyed every other tree's messages while it was at it — see the
# header. The topic is now left exactly as it is found.
#
# `cmd/topics` is idempotent and 50-jobs.sh has already run it once; this run is what guarantees the
# topic exists at all on a machine where the stack was reset. Three partitions, for the reason the
# original comment gave: the key is the aggregate, so one job's events must share a partition for
# Kafka to keep their order within it. The SHIP-135 section at the foot of this file asserts the
# whole set, the partition count, and that a second run changes nothing.
pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-topics" ./cmd/topics
popd >/dev/null
KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:29092}" \
  "$WORKDIR/shipper-topics" >"$WORKDIR/topics-recreate.log" 2>&1 \
  || { cat "$WORKDIR/topics-recreate.log"; fail "could not apply the topic set"; }
ok "topic $outbox_topic exists with the set the catalogue owns (SHIP-135), and this section leaves it alone"

# **Two fences, taken together, and neither of them is a wall clock over Kafka.**
#
# The first is the topic's per-partition end offsets, written into the file `kafka_consume_fenced`
# reads. The harness takes this fence at run start for `shipper.bid` and `shipper.delivery` and
# deliberately not for `shipper.job`, because until this ticket this section emptied that topic; it
# is taken here instead, which is *later* than run start and is what this section wants — earlier
# sections legitimately publish job events (SHIP-68 runs the real worker), and those are not this
# section's to assert on.
#
# The second is the database's own clock, and it fences the *outbox* rather than the topic. That is
# the one place a timestamp is sound: `outbox.published_at` is written by this tree's worker into
# this tree's database, and no other worktree can write a row there.
#
# Both are taken before anything publishes — no worker is running at this point, SHIP-68's having
# been waited on — so the two describe the same window from the two ends.
kafka_fence "$outbox_topic"
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
  "{\"name\":\"Verify Harness\",\"email\":\"outbox-customer-$$@example.com\",\"phone\":\"04180$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
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

# SHIP-136's rows, captured **before** the worker starts and by identifier.
#
# 61-bidding.sh and 70-delivery.sh emit bid and delivery events through the served endpoints and
# deliberately leave them unpublished; this worker run is what puts them on their topics, and the
# section after this one reads them off the wire. Taken as a list of ids rather than as a count or a
# timestamp window, because `shipper.bid` and `shipper.delivery` are **not** emptied by this file and
# are shared with every other worktree on this machine — see the SHIP-135 section's note on why a
# fence over a Kafka topic has to be an id.
ship136_ids="$("$PSQL" "$DATABASE_URL" -tAc \
  "select id from outbox
     where aggregate_type in ('bid', 'delivery') and published_at is null
     order by id;" | tr -d ' ' | grep . || true)"

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
#
# It also waits on SHIP-136's bid and delivery rows, because the section below reads them off their
# topics and this is the only worker run in the harness — a poll that stopped at four could kill the
# worker mid-pass and leave them undrained. `is null` rather than a comparison against
# `$ship136_ids`, which is the same query the other way round and shorter: this database is per
# worktree, so an unpublished bid or delivery row is this run's or an earlier run's on this tree, and
# draining either is right.
published=0
ship136_remaining=1
for _ in $(seq 1 100); do
  published="$("$PSQL" "$DATABASE_URL" -tAc \
    "select count(*) from outbox
      where aggregate_id in ('$committed_job', '$emitted_job') and published_at is not null;")"
  ship136_remaining="$("$PSQL" "$DATABASE_URL" -tAc \
    "select count(*) from outbox
      where aggregate_type in ('bid', 'delivery') and published_at is null;")"
  if [[ "$published" == "4" && "$ship136_remaining" == "0" ]]; then break; fi
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

# Read from this section's own offset fence rather than from the beginning of the topic.
#
# `--from-beginning` was correct only while this section emptied the topic first, and reading the
# *oldest* N messages of a topic nothing empties is the monotonic failure the SHIP-136 section below
# documents at length: it gets worse every run and never clears. `kafka_consume_fenced` starts each
# partition at the offset recorded above and derives its own bound from `end - fence`, so it reads
# everything appended since and stops.
kafka_consume_fenced "$outbox_topic" "$WORKDIR/outbox-consumed.json"

# What the topic holds against what the database says this worker run published — in both
# directions, which is what makes it worth doing. A row marked published that never left is the
# one failure the ordering of publish and mark exists to prevent; a message on the topic the
# outbox does not claim would mean something published without recording it.
#
# **The equality survives a shared broker by changing what it is over, not by becoming a subset.**
# The header carries the argument; the short form is that the old check drew its authority from the
# topic being empty, which is not a property one worktree can establish, and this one draws it from
# the database, which is genuinely per worktree. Three statements:
#
#   1. **completeness** — every id this run published is on the topic. This is the "at least once"
#      half, and it is the direction another tree's *delete* can still break.
#   2. **equality over what this database owns** — of the messages read, exactly those the outbox
#      knows about are exactly those this run published. Neither more nor fewer.
#   3. **provenance** — everything else read is named as another worktree's, counted, and not
#      failed. A message this database has never heard of is not evidence about this build.
#
# **As sets, deliberately.** The topic has three partitions and the key is the aggregate, so the
# consumer reads three independent streams and interleaves them however it likes. Asserting a
# single global order here would be asserting something 000004_outbox.up.sql explicitly does not
# promise — and the first run of this check did exactly that and failed, which is the shortest
# available demonstration that "ordering is per aggregate, not global" is a real property of what
# was built rather than a sentence in a comment.
"$PSQL" "$DATABASE_URL" -tAc \
  "select id from outbox where aggregate_type = 'job' and published_at > '$outbox_fence' order by id;" \
  | tr -d ' ' | grep . >"$WORKDIR/outbox-published.txt" || true
"$PSQL" "$DATABASE_URL" -tAc \
  "select id from outbox where aggregate_type = 'job' order by id;" \
  | tr -d ' ' | grep . >"$WORKDIR/outbox-known.txt" || true

python3 - "$WORKDIR/outbox-consumed.json" "$WORKDIR/outbox-published.txt" \
  "$WORKDIR/outbox-known.txt" >"$WORKDIR/outbox-provenance.txt" 2>&1 <<'PY' \
  || { cat "$WORKDIR/outbox-provenance.txt"; fail "the topic and this database's outbox do not agree"; }
import json, sys

consumed = []
for line in open(sys.argv[1]):
    if line.strip():
        consumed.append(json.loads(line)["id"])
published = {l.strip() for l in open(sys.argv[2]) if l.strip()}
known = {l.strip() for l in open(sys.argv[3]) if l.strip()}

# 1. Completeness. Every id this run published is readable off the topic.
missing = sorted(published - set(consumed))
if missing:
    sys.exit("%d of %d events this run published are not on the topic: %s"
             % (len(missing), len(published), " ".join(missing[:5])))

# 2. Equality, over the population this database can speak for. A message on the topic that this
#    outbox holds must be one this run published; anything else is a publish that was never
#    recorded, which is the failure the ordering of publish and mark exists to prevent.
ours = {i for i in consumed if i in known}
unclaimed = sorted(ours - published)
if unclaimed:
    sys.exit("%d message(s) on the topic are this database's own and are not rows this run "
             "published: %s -- something published without recording it"
             % (len(unclaimed), " ".join(unclaimed[:5])))

# 3. Provenance. Everything else is another worktree's. Counted, never failed.
print(len(set(consumed) - known))
PY
outbox_foreign="$(cat "$WORKDIR/outbox-provenance.txt")"
ok "every event this run's outbox marked published is on $outbox_topic, and every message on it this database owns is one of them"

# The third statement, reported rather than asserted. A number here is not a defect: it is the
# other worktrees on this machine, and printing it is what stops the next person reading a green
# run as evidence that the topic was theirs alone.
[[ "$outbox_foreign" =~ ^[0-9]+$ ]] || fail "the provenance split did not report a count: $outbox_foreign"
ok "and $outbox_foreign message(s) on it belong to another worktree, named rather than asserted about"

# Per aggregate, though, the order is exact — and that is the promise. Checked over every
# aggregate **this database owns** rather than only the one this section wrote, so the jobs
# domain's own events are held to it too.
#
# The narrowing from "every aggregate on the topic" is the provenance split again. Another
# worktree's messages are on this topic and may legitimately come from a build with a deliberate
# defect in it — a mutation being run, a half-finished ticket — and failing this tree's run over
# that would be reporting somebody else's experiment as this build's regression.
python3 -c '
import json, sys
known = {l.strip() for l in open(sys.argv[2]) if l.strip()}
seen = {}
for line in open(sys.argv[1]):
    if not line.strip():
        continue
    e = json.loads(line)
    if e["id"] not in known:
        continue
    previous = seen.get(e["aggregate_id"])
    if previous is not None and e["occurred_at"] < previous:
        sys.exit("%s published after a later event for the same aggregate" % e["id"])
    seen[e["aggregate_id"]] = e["occurred_at"]
' "$WORKDIR/outbox-consumed.json" "$WORKDIR/outbox-known.txt" \
  || fail "one aggregate's events are on the topic out of order"

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
ticket "SHIP-136  bid and delivery events reach their own topics, keyed on their aggregate"

# The last leg of SHIP-136: the events 61-bidding.sh and 70-delivery.sh emitted through the served
# endpoints, taken off `shipper.bid` and `shipper.delivery` by a consumer that is not this codebase.
#
# Those two sections assert the outbox rows, which is where the transactional guarantee lives.
# internal/bidding/events_test.go and internal/delivery/events_test.go assert the payloads, the retry
# paths and the rolled-back transaction. **What only this can show is that the rows the domains wrote
# publish to the right topic and are readable off it** — which before this ticket was true of
# `shipper.job` alone, and was the reason the SHIP-135 section below could break `shipper.delivery`
# freely.
#
# # It sits between SHIP-134's section and SHIP-135's on purpose
#
# The section below **deletes `shipper.delivery`** to demonstrate that a topic somebody created by
# hand with the wrong partition count is reported rather than repaired. That was free while nothing
# published to it. It is not free now, so this reads the topic first, and the note in that section
# has been corrected to say what it is now destroying.
#
# # Everything here is fenced by event id, and nothing here empties a topic
#
# `shipper.bid` and `shipper.delivery` are shared with every other worktree on this machine —
# `COMPOSE_PROJECT_NAME` is pinned, so there is one broker — and unlike `shipper.job` above, this
# file does not wipe them. So the assertion is a **subset**: every id this run published is on the
# topic. A count, or an equality against everything on the topic, would be a statement about whatever
# else the machine is doing. Docs/11 §3's SHIP-135 entry has the run that proved it.
#
# **And the consumer is fenced as well as the assertion**, which is the correction wave 9 made. A
# subset check over ids is only as good as what the consumer was allowed to see, and reading from the
# beginning of a topic nothing ever empties meant this run's events eventually fell outside the read.
# See `kafka_consume_fenced` below and the harness's own note.

if [[ -z "$ship136_ids" ]]; then
  fail "no bid or delivery events were waiting to be published; 61-bidding.sh and 70-delivery.sh emit them, so this check is proving nothing"
fi

# Read from the harness's run-start fence rather than from the beginning of the topic.
#
# **This is where the fencing rule had to be extended from the assertion to the consumer.** The
# assertion below was always fenced — a subset check over the ids this run published — and it was
# still not enough, because `--from-beginning --max-messages 500` reads the *oldest* five hundred
# messages and `shipper.bid` is emptied by nothing. Every worktree's bid events accumulate on it
# forever, so once it passes five hundred this run's own events sit past the cut and the check below
# reports them missing. That is a **monotonic** failure rather than one of this harness's
# concurrency flakes: it gets worse every run and never clears, and because 80 sorts before 90 a run
# that dies here never reaches `90-admin.sh` at all. Three wave-8 lanes met it independently.
#
# Raising the bound is the wrong instrument — a bound narrows what the assertion can see and says
# nothing about where this run's events begin. `kafka_consume_fenced` starts each partition at the
# offset the harness recorded before any product section ran, and derives its own bound from
# `end - fence`. `scripts/verify-foundation.sh` carries the full reasoning.
kafka_consume_fenced shipper.bid "$WORKDIR/bid-consumed.json"
kafka_consume_fenced shipper.delivery "$WORKDIR/delivery-consumed.json"
cat "$WORKDIR/bid-consumed.json" "$WORKDIR/delivery-consumed.json" >"$WORKDIR/ship136-consumed.json"

printf '%s\n' "$ship136_ids" >"$WORKDIR/ship136-ids.txt"

# Every id this run published, on the topic its aggregate names — and on that topic rather than
# merely somewhere. An event routed to the wrong topic is read by nobody and reports nothing.
python3 - "$WORKDIR/ship136-ids.txt" "$WORKDIR/bid-consumed.json" "$WORKDIR/delivery-consumed.json" <<'PY' \
  || fail "an event a domain emitted is not on the topic its aggregate names"
import json, sys

wanted = {line.strip() for line in open(sys.argv[1]) if line.strip()}

on = {}
for path, topic in ((sys.argv[2], "shipper.bid"), (sys.argv[3], "shipper.delivery")):
    for line in open(path):
        if not line.strip():
            continue
        e = json.loads(line)
        on[e["id"]] = (topic, e)

missing = sorted(wanted - set(on))
if missing:
    sys.exit("%d of %d events are on neither topic: %s" % (len(missing), len(wanted), " ".join(missing[:5])))

wrong = [
    "%s is a %s event on %s" % (i, on[i][1]["aggregate_type"], on[i][0])
    for i in wanted
    if on[i][0] != "shipper." + on[i][1]["aggregate_type"]
]
if wrong:
    sys.exit("; ".join(wrong[:5]))

PY
ok "every bid and delivery event this run emitted is on shipper.bid or shipper.delivery, on the topic its aggregate names"

# The type and the version, off the wire. Both halves matter: a consumer branches on `type`, and
# `schema_version` is what lets it refuse a shape it was not built for. The types are listed rather
# than counted, so a section that quietly stopped exercising one is reported.
#
# **There are ten bid and delivery event types and this enumerates nine of them.** `bid.expired`
# arrived with SHIP-89, after this list was written. It is not missing coverage: the row is in
# `$ship136_ids` like every other, so it is read off the topic and the version loop below holds it to
# `1 / 1` exactly as it holds the nine — and 61-bidding.sh already asserts its existence and its
# payload, fenced on the one bid. What the list does is assert *presence*, which is a statement about
# the sections upstream still emitting each type, and it is left at the nine SHIP-136's *Done when*
# names rather than widened here. Adding `"bid.expired"` to the set below is a one-line change and a
# strictly stronger check; it belongs to whoever wants SHIP-89's presence guarded from this file too.
python3 - "$WORKDIR/ship136-ids.txt" "$WORKDIR/ship136-consumed.json" <<'PY' \
  || fail "the events on the topic are not the ones SHIP-136 says this platform emits"
import json, sys

wanted = {line.strip() for line in open(sys.argv[1]) if line.strip()}
seen = {}
for line in open(sys.argv[2]):
    if not line.strip():
        continue
    e = json.loads(line)
    if e["id"] in wanted:
        seen[e["type"]] = e

expected = {
    "bid.placed", "bid.revised", "bid.withdrawn",
    "bid.countered", "bid.accepted", "bid.rejected",
    "delivery.driver_assigned", "delivery.milestone_recorded", "delivery.proof_recorded",
}
absent = sorted(expected - set(seen))
if absent:
    sys.exit("nothing on the topic for: %s" % " ".join(absent))

for name, e in sorted(seen.items()):
    if e.get("schema_version") != 1 or e["payload"].get("schema_version") != 1:
        sys.exit("%s is on the wire at envelope %s / payload %s, want 1 / 1"
                 % (name, e.get("schema_version"), e["payload"].get("schema_version")))

PY
ok "all nine bid and delivery event types reach a consumer, each carrying its schema version in the envelope and in the payload"

# The budget, one last time, on the bytes a consumer actually receives. The tripwire exists in three
# places now — the catalogue test in cmd/api, the closed key sets in the two domains' tests, and here
# — because this is the only one that reads what left the building.
if grep -qi 'budget' "$WORKDIR/ship136-consumed.json"; then
  fail "a message on shipper.bid or shipper.delivery mentions a budget"
fi
ok "and nothing on either topic mentions a budget, which is the last point at which it could have been redacted"

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
# produced from a topic it had just emptied. The one topic it manipulates is **shipper.delivery**.
#
# **That used to be free and is not any more.** The original note read "which no code publishes to
# yet — deliberately, so that breaking a topic on purpose cannot cost another section anything", and
# SHIP-136 ended it: `internal/delivery` now emits three events onto that topic. So the SHIP-136
# section is placed **above** this one rather than below, and has already read what it needs off
# shipper.delivery before the deletion here destroys it. Whoever adds the next Kafka assertion should
# read that ordering as load-bearing: there is no longer a topic in the set that nothing publishes
# to, so a section that breaks one has to run after every section that reads it.

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

# Staged on shipper.delivery, whose messages the SHIP-136 section above has already read — see this
# section's header for why the ordering matters now. This is the failure the command
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

# **Fenced per partition, like every other read in this file since wave 10.**
#
# This was `--from-beginning --max-messages 200`, and it worked only because the SHIP-134 section
# above emptied the topic first. That section no longer does — five worktrees share the broker and
# the wipe destroyed their messages — so an unfenced read here would be the *oldest* two hundred
# messages of a topic nothing ever empties, which is the monotonic failure the SHIP-136 section
# documents: it gets worse every run and never clears.
#
# `kafka_consume_fenced` cannot be reused because headers are not part of what it prints, so this
# is the same shape open-coded: start each partition at the recorded fence, bound by `end - fence`.
headers=""
while read -r partition end; do
  start="$(awk -v p="$partition" '$1 == p { print $2 }' "$WORKDIR/kafka-fence-$outbox_topic.txt")"
  start="${start:-0}"
  (( start > end )) && start=0
  (( end - start > 0 )) || continue
  headers+="$("${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-console-consumer.sh" \
    --bootstrap-server localhost:9092 --topic "$outbox_topic" \
    --partition "$partition" --offset "$start" --property print.headers=true \
    --max-messages "$((end - start))" --timeout-ms 15000 </dev/null 2>/dev/null | tr -d '\r' || true)"
  headers+=$'\n'
done < <(kafka_end_offsets "$outbox_topic")

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
# payload's field set, one line per event, derived from the payload structs by reflection.
#
# **The list said twelve and the golden has held thirteen since SHIP-89 added `bid.expired`.** Stale
# prose rather than lost coverage — the event was already read off the topic and held to its schema
# version by the SHIP-136 section — but a list that undercounts is a list nobody trusts to be
# complete, so the event is enumerated here too and the count corrected. A
# payload that changes shape moves a line; a line that moved without its `v` moving is the defect
# the file exists to make visible in review.
events_golden="$ROOT/services/core/cmd/api/events_golden.txt"
for event in \
  job:job.status_changed job:job.expiry_warned job:job.expiry_extended \
  bid:bid.placed bid:bid.revised bid:bid.withdrawn \
  bid:bid.countered bid:bid.accepted bid:bid.rejected bid:bid.expired \
  delivery:delivery.driver_assigned delivery:delivery.milestone_recorded \
  delivery:delivery.proof_recorded; do
  grep -qE "^shipper\.${event%%:*} +${event#*:} +v[0-9]+ +[0-9]+ +[a-z_]+:" "$events_golden" \
    || fail "${event#*:} has no schema recorded in cmd/api/events_golden.txt"
done
ok "each of the thirteen domain events has a recorded schema: topic, version, bound and field set"

# The budget-privacy invariant, applied to the catalogue rather than to one payload. An event is
# the worst place for it to appear, because it travels past every point a response body could have
# redacted it (Docs/01 §4.3).
if grep -qi "budget" "$events_golden"; then
  fail "an event in the catalogue declares a budget field"
fi
ok "and no event in the catalogue declares a budget field, in this domain or any later one"

unset outbox_foreign ours_in_order emitted_type consumed_payload partition end start
unset published ship136_remaining ship136_ids worker_pid outbox_fence outbox_token emitted_job emitted_rows
unset committed_job rolled_back_job rolled_back_rows outbox_topic
unset event_ids
unset described retention_stated stored_version wire_version headers events_golden event topic
unset emitted_event_id header_line id
unset -f outbox_request topics_apply topic_described
