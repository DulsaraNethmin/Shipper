# shellcheck shell=bash
#
# SHIP-7 — the migration tool, up and down, against a real database.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 00–09, so this runs before the service starts — deliberately, because it takes the schema
# all the way down and back up again, and a service holding a pool through that would be
# querying tables that briefly do not exist. It leaves the database migrated.

ticket "SHIP-7  migrations run up and down"

pushd "$ROOT/services/core" >/dev/null
export DATABASE_URL

go run ./cmd/migrate down all >/dev/null 2>&1 || true   # start from a known state

go run ./cmd/migrate up >/dev/null
version_after_up="$(go run ./cmd/migrate version)"
# The highest number on disk, rather than a literal. Migrations are allocated in per-domain
# blocks (migrations/blocks.go), so the newest one is not the count of them and hard-coding
# either would make this line a chore to update on every schema change.
highest_migration="$(ls migrations/*.up.sql | sed -E 's#.*/0*([0-9]+)_.*#\1#' | sort -n | tail -1)"
[[ "$version_after_up" == "version $highest_migration" ]] \
  || fail "after up, expected 'version $highest_migration', got '$version_after_up'"
ok "migrate up applied every migration, ending at $highest_migration"

"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from pg_proc where proname = 'set_updated_at';" | grep -q 1 \
  || fail "set_updated_at() was not created"
"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from pg_extension where extname = 'citext';" | grep -q 1 \
  || fail "citext extension was not created"
ok "the migration's objects exist in the database"

go run ./cmd/migrate down all >/dev/null
version_after_down="$(go run ./cmd/migrate version)"
[[ "$version_after_down" == "no migrations applied" ]] \
  || fail "after down, expected 'no migrations applied', got '$version_after_down'"
ok "migrate down reversed every one of them"

if "$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from pg_proc where proname = 'set_updated_at';" | grep -q 1; then
  fail "set_updated_at() survived the rollback"
fi
ok "the migration's objects are gone after rollback"

go run ./cmd/migrate up >/dev/null   # leave the database migrated
popd >/dev/null
