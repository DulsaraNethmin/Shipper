#!/usr/bin/env bash
#
# Demonstrates the "Done when" criterion of every ticket that reaches an HTTP endpoint or a
# database constraint. The list is printed at the end of a successful run, collected from the
# sections themselves — an earlier version of this file carried it as a literal on the last
# line, and that literal said "SHIP-1..SHIP-15" long after it had stopped being true.
#
# Docs/09 makes the acceptance criterion the definition of done: if it cannot be shown,
# the ticket is not finished. This script is how it gets shown — on a developer machine
# now, and from CI once SHIP-20 lands.
#
#   make up && make verify
#
# Exits non-zero on the first failure.
#
# A successful run also holds Docs/11 §3's check count to what it just measured, and
# `make verify-update` (or `--update`) rewrites that figure rather than a person retyping it.
# See "the check count" at the foot of this file.
#
# # This file is the harness. The checks are in scripts/verify/
#
# Everything below is shared: the environment, ticket/ok/fail/json, post_json, the token
# minting, the service lifecycle, $pass and the summary. The checks themselves live one file
# per milestone or domain in scripts/verify/, sourced in lexical order, exactly as the root
# Makefile includes mk/*.mk (Docs/10 §9.2).
#
# That is the point of the split and it is worth stating plainly: **a track adds a file here
# and edits none.** Until SHIP-15e this was 1148 lines in one file, which was tolerable while
# one track at a time added endpoints. Wave 3 has two.
#
# A section is `scripts/verify/NN-<name>.sh`. The number decides when it runs, and the ranges
# are reserved the same way migration numbers are (services/core/migrations/blocks.go), so two
# branches cannot draw the same one:
#
#   00–09  the local stack — runs BEFORE the service is built. No listening port yet
#   10–19  the Go service itself: health, configuration, logging
#   20–29  package boundaries and the import lint
#   30–39  HTTP: the /v1 group, the error contract, request IDs, idempotency, authentication
#   40–49  identity        (M1)
#   50–59  jobs            (M2)
#   60–69  fleet, bidding  (M3)
#   70–79  delivery        (M4)
#   80–89  notifications   (M5)
#   90–94  admin           (M6)
#   95–99  hardening       (M7)
#
# Sections are sourced, not executed, so they run in this shell: every helper and variable here
# is in scope, each ok() counts towards one total, and a fail() anywhere ends the run.
#
# A file that is not named NN-<name>.sh is refused rather than skipped. A section that is
# silently not run is the same defect as a route dropped in a merge — no error, no failure,
# and an acceptance criterion that has quietly stopped being demonstrated.
#
# # Starting cmd/worker from a section — read this before writing one that does
#
# **cmd/worker is one binary, and every start runs every registered task.** There is no way to
# start one: the scheduler builds the whole manifest and runs a pass of each immediately at
# start-up, which is what makes a start cheap enough to demonstrate anything with. So a section
# that runs the worker to show job expiry also drains the outbox, also sweeps for expiry warnings,
# and will also run whatever the next ticket registers.
#
# **Five tasks are registered today**, counted from the registry rather than from the last version
# of this comment — `job-expiry`, `job-expiry-warning` and `job-auto-complete` (tasks_jobs.go),
# `bid-expiry` (tasks_bidding.go) and `outbox-publisher` (tasks_outbox.go). SHIP-89 added the fourth
# in wave 8 and SHIP-119 the fifth in wave 9, which is both of the two this comment used to name as
# still to come.
#
# Do not maintain that count by hand. `cmd/worker/manifest_test.go`'s
# `TestTheRegisteredTaskSetIsWhatItSaysItIs` pins the five names against the real registry, so a
# sixth registration fails there with the open selector question attached — which is what makes
# adding one a decision somebody re-read rather than something that happened. This comment went
# stale for two consecutive waves before that test existed, and a count in prose cannot see itself
# go out of date.
#
# **`outbox-publisher` is the one that does not fit rule 1 below**, and a section author has to know
# it: every unpublished row is due the moment it is written, so "leave nothing due that you are not
# demonstrating" is unsatisfiable against it. `scripts/verify/61-bidding.sh` works around it by
# pointing its worker at an unreachable broker so every outbox pass fails legibly and leaves the
# rows claimable. Docs/11 §9 carries why that workaround is still load-bearing.
#
# **SHIP-15r settled how a section deals with that, and the answer is a convention rather than a
# selector on the binary.** The reasoning is in Docs/11 §3; the rule is two lines and a section
# author has to follow both:
#
#   1. **Fence what you assert on.** Every query must be keyed to a row this section created — a
#      job id, a bid id, an object key. Never a count over a table, never a window over
#      `published_at`, never "the most recent". Another section's worker start, another worktree's
#      `make verify` and this run's own second pass are all in the same database and the same Kafka
#      topic, and none of them is ordered against you.
#   2. **Own what you assert about.** Before starting the worker, know what your fixtures look like
#      to every *other* registered task, because fencing protects an assertion and not a fixture. A
#      job left `Open` an hour from its deadline is claimed by the expiry-warning sweep whether or
#      not the section mentions warnings. Leave nothing due that you are not demonstrating.
#
# The instances that produced the rule are worth knowing. SHIP-134's outbox assertion was a count
# over every job event the database had ever held, and SHIP-68's section broke it at the wave-4
# merge by draining the outbox as a side effect; two wave-5 sections then met the same shape from
# the other direction. All three resolved to fencing on ids, and it has held every time since.
#
# **The alternative was a `--only=<task>` flag on the binary**, and it was rejected for two
# reasons. A section demonstrating one task alone stops demonstrating that the tasks coexist, which
# is the deployment's actual shape and the only place a claim-loop interaction would ever surface.
# And it would be a mechanism with one consumer at a time, in a repository whose recurring defect —
# Docs/11 §9 lists four — is a documented mechanism nothing exercises. Reopen it if a task ever
# sweeps rows that are due by wall-clock alone, because that is the one case fencing cannot cover.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT="$PWD"

# --update rewrites Docs/11 §3's check count with what this run measures, the way
# `go test ./cmd/api -run TestErrorCodeDocumentIsCurrent -update` rewrites the error code list.
# Without it the figure is checked and a disagreement fails the run.
UPDATE_TRACKER=0
for arg in "$@"; do
  case "$arg" in
    --update) UPDATE_TRACKER=1 ;;
    *) echo "usage: $0 [--update]" >&2; exit 2 ;;
  esac
done

# shellcheck disable=SC1091
[[ -f deploy/.env ]] && source deploy/.env

POSTGRES_USER="${POSTGRES_USER:-shipper}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-shipper}"
POSTGRES_DB="${POSTGRES_DB:-shipper}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
REDIS_PORT="${REDIS_PORT:-6379}"
DATABASE_URL="${DATABASE_URL:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable}"
REDIS_URL="${REDIS_URL:-redis://localhost:${REDIS_PORT}/0}"

# The object store (SHIP-15p). Every default matches deploy/.env.example, internal/config and
# deploy/docker-compose.yml; `make verify` exports whatever deploy/.env overrides, so a worktree
# with its own bucket is checked against its own bucket.
MINIO_PORT="${MINIO_PORT:-9000}"
STORAGE_ENDPOINT="${STORAGE_ENDPOINT:-http://localhost:${MINIO_PORT}}"
STORAGE_BUCKET="${STORAGE_BUCKET:-shipper-dev}"
STORAGE_REGION="${STORAGE_REGION:-ap-southeast-2}"
STORAGE_ACCESS_KEY_ID="${STORAGE_ACCESS_KEY_ID:-shipper}"
STORAGE_SECRET_ACCESS_KEY="${STORAGE_SECRET_ACCESS_KEY:-shipperminio}"

# A port of its own, so the run is not affected by whatever is already bound locally and
# so SHIP-5's "listens on a configured port" is actually being exercised.
VERIFY_PORT="${VERIFY_PORT:-18080}"

COMPOSE=(docker compose -f deploy/docker-compose.yml)
KAFKA_BIN=/opt/kafka/bin
PSQL="$(command -v psql || echo "$(brew --prefix libpq 2>/dev/null)/bin/psql")"

SECTIONS="scripts/verify"
TRACKER="Docs/11-delivery-status.md"

WORKDIR="$(mktemp -d)"
SERVER_PID=""

# Set to 1 by the final line of this file, and read by cleanup() below.
#
# # Why a sentinel rather than the exit status
#
# `set -euo pipefail` and `trap cleanup EXIT` together have a failure mode on bash 3.2.57 — what
# `/usr/bin/env bash` resolves to on macOS, and what CI's runner and every developer machine here
# use. **An unbound-variable abort leaves no pending status**, so cleanup()'s successful `rm -rf`
# became the script's own and `make verify` reported **exit 0 having run no checks and printed no
# count line.** A harness that reports success without running is worse than one that fails.
#
# Two things measured about it, both of which cost somebody an hour if they are not written down:
#
#   - **`set -e` is required.** With `set -uo pipefail` and no `-e`, the same abort exits 1
#     correctly. Anybody "fixing" this by reaching for `-u` would change nothing.
#   - **`local status=$?; …; return $status` is inert, and a reviewer would approve it.** `$?` is
#     already 0 at trap entry — probed directly, the trap prints `$?=0` on a script that then exits
#     0. An ERR trap does not fire either. The status is destroyed before any handler runs, so
#     there is nothing to read and the only thing left is to record, positively, that the end was
#     reached.
#
# Verified on all five exit paths: `set -u` abort → 1, `set -e` abort → 1, fail()'s `exit 1` → 1,
# the stale-count `exit 1` → 1, a clean run → 0.
#
# **This closes the false pass and not the false failure.** Two harnesses across two worktrees still
# break each other deterministically (CLAUDE.md, "Working in more than one branch at once"), and no
# sentinel can help with that. The exit status is not a verdict in either direction — read the
# "N checks passed across M sections" line.
VERIFY_FINISHED=0

cleanup() {
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"

  # Last, and after the cleaning rather than before it: an abort still gets its server killed and
  # its working directory removed, and only then is the status forced. See VERIFY_FINISHED above.
  [[ "$VERIFY_FINISHED" == 1 ]] || exit 1
}
trap cleanup EXIT

# --- the harness -------------------------------------------------------------------------

pass=0
tickets=""

# ticket <ID>  <description> — announces a section and records the ticket for the summary.
#
# The ID is the first word, which is where every existing call already puts it, so nothing
# needs declaring twice and the summary cannot drift from what actually ran.
ticket() {
  printf '\n\033[1m=== %s ===\033[0m\n' "$*"
  local id="${1%% *}"
  case " $tickets " in
    *" $id "*) ;;
    *) tickets="$tickets $id" ;;
  esac
}

ok()     { printf '  \033[32m✓\033[0m %s\n' "$*"; pass=$((pass + 1)); }
fail()   { printf '  \033[31m✗\033[0m %s\n' "$*"; exit 1; }

# json <file> <expr> — read a value out of a JSON document with python3.
json() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))'"$2"')' "$1"; }

# post_json <key> <path> <body> <outfile> — one state-changing request, answering with its status.
#
# Every state-changing endpoint refuses a request with no Idempotency-Key (SHIP-15), so the key
# is an argument rather than an option: a section that forgets one gets a 400 that has nothing
# to do with what it was testing.
post_json() {
  curl -s -X POST -o "$4" -w '%{http_code}' \
    -H "Idempotency-Key: $1" -H 'Content-Type: application/json' \
    -d "$3" "http://localhost:$VERIFY_PORT$2"
}

# The header name is spelled by RFC 9110 and not by us.
auth_header="Authorization"  # spelling:ok — HTTP header name, RFC 9110

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

# mint_token <subject> — an access token for a caller that does not exist.
#
# Signed with the development key from deploy/.env.example, which is public and in this
# repository on purpose; the service refuses that key outside development. It is in the harness
# rather than in the section that first needed it (SHIP-44) because every protected route from
# SHIP-46 onwards needs the same thing, and two copies of a signing routine is two answers to
# what a valid token looks like.
mint_token() {
  local sub="$1" now exp header payload signing_input signature
  now="$(date +%s)"
  exp=$((now + 900))
  header='{"alg":"HS256","typ":"JWT","kid":"dev"}'
  payload="{\"sub\":\"$sub\",\"role\":\"customer\",\"sid\":\"$(uuidgen | tr 'A-Z' 'a-z')\",\"iat\":$now,\"exp\":$exp,\"jti\":\"$(uuidgen | tr 'A-Z' 'a-z')\",\"iss\":\"shipper\",\"aud\":\"shipper-mobile\"}"
  signing_input="$(printf '%s' "$header" | b64url).$(printf '%s' "$payload" | b64url)"
  signature="$(printf '%s' "$signing_input" \
    | openssl dgst -sha256 -hmac "shipper-local-development-signing-key-not-a-secret" -binary \
    | b64url)"
  printf '%s.%s' "$signing_input" "$signature"
}

# wait_for_health — block until the service answers, or give up after ten seconds.
#
# Used by the start below and by any section that restarts the service with different
# configuration (SHIP-167 does). Answering "is it up yet" in two places is how one of them ends
# up with a shorter timeout than the machine needs.
wait_for_health() {
  for _ in $(seq 1 50); do
    curl -fsS "http://localhost:$VERIFY_PORT/health" >/dev/null 2>&1 && return 0
    sleep 0.2
  done
  return 1
}

# --- reading a shared Kafka topic ------------------------------------------------------------
#
# **A fence has to protect the consumer, not only the assertion.**
#
# The existing rule — fence on an id, never a timestamp — is about what a check *asserts*. It says
# nothing about what the consumer is allowed to *see*, and that turned out to be the other half of
# the same problem. `shipper.bid` is shared by every worktree on this machine and **nothing deletes
# it**: 80-notifications.sh recreates `shipper.job` on every run and SHIP-135's section recreates
# `shipper.delivery`, and no section has ever touched `shipper.bid`. So it grows, run after run,
# forever.
#
# A consumer reading `--from-beginning --max-messages 500` therefore reads the *oldest* five hundred
# messages on it. Once the topic passes five hundred, this run's own events are past the cut and the
# section fails — and it fails on a tree where nothing is wrong. **This failure is monotonic rather
# than intermittent**, which is what distinguishes it from the concurrency false-failures elsewhere
# in this file: it gets worse with every run and never better, and three separate lanes in wave 8
# each hit it and each read it as a flake. Because 80 sorts before 90, a run that dies there never
# reaches `90-admin.sh` at all, so a check count from such a run says nothing about admin.
#
# **Raising `--max-messages` is the wrong instrument and was tried.** A bound narrows what the
# assertion can see; it does not say where this run's events start. Wave 8 raised it to fifty
# thousand and reverted it, correctly.
#
# **The fence is the topic's end offsets, captured before any product section runs.** Everything
# this run publishes lands after them, so a consumer starting there reads a superset of this run's
# events and nothing that predates it — and the assertions stay subset checks over ids, because
# another worktree publishing concurrently is still not ordered against this one.
#
# It is captured at run start rather than at the top of the section that reads, and that is a
# deliberate margin rather than a necessity today. Today the only producer onto those two topics is
# 80-notifications.sh's own worker run: 61-bidding.sh points its worker at `localhost:1` precisely so
# the outbox pass fails and leaves the rows for that section, and 50-jobs.sh's worker runs before any
# bid or delivery row exists. **One future task registered in `cmd/worker` changes that**, and a fence
# taken at the top of the reading section would then be behind the events it needs, silently. Taking
# it at run start costs one command and cannot go stale that way.
#
# `kafka-get-offsets.sh` is the instrument that answers this. `GetOffsetShell` through
# `kafka-run-class` returns nothing usable — do not reach for it.

# kafka_end_offsets <topic> — "<partition> <end-offset>" per line, ascending by partition.
#
# Prints nothing at all for a topic that does not exist, which is the right answer: a topic that did
# not exist when the fence was taken holds nothing this run has to read past.
#
# The `|| true` is load-bearing under this file's `set -o pipefail`. `kafka-get-offsets.sh` exits
# non-zero for a topic the broker does not have, and a bare pipeline would propagate that and end the
# run — which is exactly the case the empty output is meant to handle. Braced so the failure is
# swallowed at the producing end rather than by the pipeline as a whole, where it would also mask a
# broken `awk`.
kafka_end_offsets() {
  { "${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-get-offsets.sh" \
      --bootstrap-server localhost:9092 --topic "$1" --time -1 </dev/null 2>/dev/null || true; } \
    | tr -d '\r' | awk -F: 'NF == 3 && $3 ~ /^[0-9]+$/ { print $2, $3 }' | sort -n
}

# kafka_fence <topic>… — record where each topic ends now.
kafka_fence() {
  local topic
  for topic in "$@"; do
    kafka_end_offsets "$topic" >"$WORKDIR/kafka-fence-$topic.txt"
  done
}

# kafka_consume_fenced <topic> <outfile> — every message appended to <topic> since its fence,
# one per line, across every partition.
#
# **The bound is derived rather than chosen.** Each partition is read for exactly
# `end - fence` messages, recomputed from the broker at read time, so the consumer stops as soon as
# it has them instead of sitting out a timeout — and a partition with nothing new is not read at
# all. Messages another worktree appends after that end offset are simply not read, which is
# harmless: every assertion over this file is a subset check on ids this run created.
#
# Only one partition can be read per invocation, so a three-partition topic is three reads. `-T`
# still attaches stdin, hence `</dev/null` on the consumer — without it the console consumer eats
# the loop's own input and the second partition is never read.
kafka_consume_fenced() {
  local topic="$1" out="$2" fence="$WORKDIR/kafka-fence-$1.txt"
  local partition end start count
  : >"$out"
  while read -r partition end; do
    start=0
    if [[ -f "$fence" ]]; then
      start="$(awk -v p="$partition" '$1 == p { print $2 }' "$fence")"
      start="${start:-0}"
    fi
    # A topic deleted and recreated since the fence restarts at zero, so a fence beyond the end is
    # a fence for a topic that no longer exists. Read the whole partition rather than nothing.
    (( start > end )) && start=0
    count=$((end - start))
    (( count > 0 )) || continue
    "${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-console-consumer.sh" \
      --bootstrap-server localhost:9092 --topic "$topic" \
      --partition "$partition" --offset "$start" \
      --max-messages "$count" --timeout-ms 15000 </dev/null 2>/dev/null \
      | tr -d '\r' >>"$out" || true
  done < <(kafka_end_offsets "$topic")
}

# --- the sections --------------------------------------------------------------------------

# Every entry is checked before anything runs, so a misnamed file is a message rather than a
# section that quietly did not happen.
section_count=0
for entry in "$SECTIONS"/*; do
  [[ -e "$entry" ]] || fail "$SECTIONS holds no sections — the checks live there, not in this file"
  base="${entry##*/}"
  case "$base" in
    [0-9][0-9]-*.sh) section_count=$((section_count + 1)) ;;
    *) fail "$SECTIONS/$base is not named NN-<name>.sh, so it would never run — see the range table in $0" ;;
  esac
done

# run_sections <lowest> <highest> — source every section whose number is in the range.
run_sections() {
  local lower="$1" upper="$2" file base prefix
  for file in "$SECTIONS"/*.sh; do
    base="${file##*/}"
    prefix="${base%%-*}"
    (( 10#$prefix >= lower && 10#$prefix <= upper )) || continue
    # shellcheck source=/dev/null
    source "$file"
  done
}

run_sections 0 9

# The Kafka fence, taken here because 00-stack.sh has just proved the broker is up and because
# nothing that follows has published anything yet. `shipper.job` is not fenced: 80-notifications.sh
# deletes and recreates it, so `--from-beginning` there is already a statement about this run.
# See "reading a shared Kafka topic" above for why this is at run start rather than in the section
# that reads.
kafka_fence shipper.bid shipper.delivery

# ---------------------------------------------------------------------------------------
ticket "SHIP-5  service builds and listens on a configured port"

pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-api" \
  -ldflags "-X github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo.version=verify-1.0.0 \
            -X github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo.commit=$(git rev-parse HEAD) \
            -X github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo.builtAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  ./cmd/api
popd >/dev/null
ok "service built"

SHIPPER_ENV=development \
HTTP_PORT="$VERIFY_PORT" \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
  "$WORKDIR/shipper-api" >"$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

wait_for_health \
  || { cat "$WORKDIR/server.log"; fail "service did not come up on port $VERIFY_PORT"; }
ok "listening on HTTP_PORT=$VERIFY_PORT, which is not the default"

run_sections 10 99

# ---------------------------------------------------------------------------------------
ticket "SHIP-5  graceful shutdown"

kill -TERM "$SERVER_PID"
for _ in $(seq 1 50); do
  kill -0 "$SERVER_PID" 2>/dev/null || break
  sleep 0.2
done
wait "$SERVER_PID" 2>/dev/null || true
SERVER_PID=""
grep -q "stopped cleanly" "$WORKDIR/server.log" || fail "the service did not shut down cleanly on SIGTERM"
ok "drains and stops cleanly on SIGTERM"

# --- what was demonstrated -----------------------------------------------------------------
#
# Collected from the ticket() calls rather than written down, so adding a section adds its
# ticket here and no track has to edit this line. Sorted numerically on the part after the
# dash, which keeps SHIP-15, SHIP-15a and SHIP-15c together and puts SHIP-149 after SHIP-45 —
# the letter suffixes then order among themselves on sort's last-resort whole-line comparison.
#
# Deliberately no -u: with a key given, sort -u drops lines whose *keys* match rather than
# whose lines do, which discards SHIP-15a as a duplicate of SHIP-15 and X-1 as a duplicate of
# SHIP-1. ticket() has already deduplicated, so there is nothing left for -u to do.

# Unquoted on purpose: $tickets is a space-separated list and the split is what is wanted.
# shellcheck disable=SC2086
demonstrated="$(printf '%s\n' $tickets | sort -t- -k2,2n | tr '\n' ' ')"

printf '\n\033[32m%s checks passed across %s sections.\033[0m\n' "$pass" "$section_count"
printf '\033[32mDemonstrated: %s\033[0m\n\n' "${demonstrated% }"

# --- the check count in Docs/11 §3 (SHIP-15i) ------------------------------------------------
#
# That figure had been a hand-typed scalar in prose, and prose is the one shape a merge cannot
# resolve: it conflicted in four consecutive merges and in three of them *no* figure in the
# conflict was correct — including develop's own, already stale by twenty before one merge began.
# Every other shared surface a wave touches is merge=union, generated, or held sorted by a test.
# This one was none of those, which is exactly why it was the one that kept failing.
#
# It is *checked* rather than generated, and that is the decision rather than an accident. The
# figure lives in a sentence somebody reads, so generating the line would mean owning its
# wording forever; comparing two numbers costs nothing and catches all four of the merges.
# `--update` is the escape hatch, and it rewrites only the two numbers — the same shape as
# `go test ./cmd/api -run TestErrorCodeDocumentIsCurrent -update` and routes_golden.txt.
#
# It runs last, after everything has passed, so the number it writes is a number every check
# stood behind.

count_pattern='\*\*[0-9]+ checks across [0-9]+ sections\*\*'

[[ -f "$TRACKER" ]] || fail "$TRACKER is missing, and it is where the check count is recorded"

matches="$(grep -cE "$count_pattern" "$TRACKER" || true)"
case "$matches" in
  1) ;;
  0) fail "$TRACKER no longer says \"**N checks across M sections**\" anywhere.
     This run measured $pass across $section_count. Put the sentence back in §3, or move
     this guard with it — a figure nothing checks is the figure that was wrong four times." ;;
  *) fail "$TRACKER states the check count $matches times, so a reader cannot tell which is
     current and --update would rewrite them all. Leave exactly one, in §3." ;;
esac

stated="$(grep -oE "$count_pattern" "$TRACKER")"
[[ "$stated" =~ ([0-9]+)\ checks\ across\ ([0-9]+)\ sections ]] \
  || fail "cannot read the two numbers out of $stated"
stated_checks="${BASH_REMATCH[1]}"
stated_sections="${BASH_REMATCH[2]}"

# Neither branch calls ok(): $pass is the figure being written, and a check that counted itself
# would make the number one larger than the run it describes.
if [[ "$UPDATE_TRACKER" == 1 ]]; then
  awk -v checks="$pass" -v sections="$section_count" '
    match($0, /\*\*[0-9]+ checks across [0-9]+ sections\*\*/) {
      printf "%s**%s checks across %s sections**%s\n", \
        substr($0, 1, RSTART - 1), checks, sections, substr($0, RSTART + RLENGTH)
      next
    }
    { print }
  ' "$TRACKER" >"$WORKDIR/tracker.md"

  grep -qE "\*\*$pass checks across $section_count sections\*\*" "$WORKDIR/tracker.md" \
    || fail "the rewrite did not produce the measured figure; $TRACKER is untouched"

  # Written back through the existing file rather than moved over it, so the document keeps
  # its own permissions and its inode.
  cat "$WORKDIR/tracker.md" >"$TRACKER"

  if [[ "$stated_checks" == "$pass" && "$stated_sections" == "$section_count" ]]; then
    printf '\033[32m%s §3 already stated %s checks across %s sections.\033[0m\n\n' \
      "$TRACKER" "$pass" "$section_count"
  else
    printf '\033[32m%s §3 updated: %s across %s (was %s across %s).\033[0m\n\n' \
      "$TRACKER" "$pass" "$section_count" "$stated_checks" "$stated_sections"
  fi
elif [[ "$stated_checks" != "$pass" || "$stated_sections" != "$section_count" ]]; then
  printf '\033[31m%s §3 says %s checks across %s sections. This run measured \033[1m%s across %s\033[0m.\n' \
    "$TRACKER" "$stated_checks" "$stated_sections" "$pass" "$section_count"
  printf 'Write the measured figure — it is never reconciled, and never resolved by taking a side:\n'
  printf '    make verify-update\n\n'
  exit 1
else
  printf '\033[32m%s §3 states the figure this run measured.\033[0m\n\n' "$TRACKER"
fi

# The end was reached. Nothing below this line, and nothing that can fail above it without
# cleanup() turning the exit status into 1. See VERIFY_FINISHED at the head of the file.
VERIFY_FINISHED=1
