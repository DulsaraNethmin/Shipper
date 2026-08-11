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

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT="$PWD"

# shellcheck disable=SC1091
[[ -f deploy/.env ]] && source deploy/.env

POSTGRES_USER="${POSTGRES_USER:-shipper}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-shipper}"
POSTGRES_DB="${POSTGRES_DB:-shipper}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
REDIS_PORT="${REDIS_PORT:-6379}"
DATABASE_URL="${DATABASE_URL:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable}"
REDIS_URL="${REDIS_URL:-redis://localhost:${REDIS_PORT}/0}"

# A port of its own, so the run is not affected by whatever is already bound locally and
# so SHIP-5's "listens on a configured port" is actually being exercised.
VERIFY_PORT="${VERIFY_PORT:-18080}"

COMPOSE=(docker compose -f deploy/docker-compose.yml)
KAFKA_BIN=/opt/kafka/bin
PSQL="$(command -v psql || echo "$(brew --prefix libpq 2>/dev/null)/bin/psql")"

SECTIONS="scripts/verify"

WORKDIR="$(mktemp -d)"
SERVER_PID=""

cleanup() {
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"
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
