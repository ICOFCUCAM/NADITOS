#!/usr/bin/env bash
# migrate_roundtrip.sh
#
# Verifies that the migration set is round-trippable:
#
#   1. Apply every up migration → snapshot schema A.
#   2. Apply every down migration in reverse → schema must be empty
#      (only the schema_migrations bookkeeping table left).
#   3. Apply every up migration again → snapshot schema B.
#   4. Diff A vs B. They must be byte-identical; if they aren't, a down
#      migration silently leaks state (orphan columns, lingering triggers)
#      and re-deploying from a backup would produce a different database.
#
# Pre-existing migrations that don't have a matching .down.sql file are
# treated as a hard failure — we never want to merge an up that can't
# be rolled back, because that turns a bad deploy into a manual rescue.
#
# DATABASE_URL must point at an EMPTY database. The script will leave
# the database fully migrated when it succeeds.
set -euo pipefail

DIR="$(cd "$(dirname "$0")/.." && pwd)"
MIG="$DIR/db/migrations"
DB_URL="${DATABASE_URL:?DATABASE_URL must be set}"

# 1) Pair check — every .up.sql must have a sibling .down.sql.
missing=0
for f in "$MIG"/*.up.sql; do
  v="$(basename "$f" .up.sql)"
  if [ ! -f "$MIG/$v.down.sql" ]; then
    echo "missing down migration for $v" >&2
    missing=1
  fi
done
[ "$missing" = "0" ] || exit 1

# pg_dump_schema dumps a sorted, normalised schema for diffing. We strip
# the comment block at the top (timestamps + pg_dump version) and any
# blank lines so cosmetic dump variations don't trip the diff. The order
# of CREATE/ALTER stays meaningful because pg_dump emits objects in a
# dependency-stable order.
pg_dump_schema() {
  local out="$1"
  pg_dump --schema-only --no-owner --no-privileges --no-comments "$DB_URL" \
    | grep -v '^--' \
    | awk 'NF' \
    > "$out"
}

run_migrate() {
  DATABASE_URL="$DB_URL" "$DIR/scripts/migrate.sh" "$1"
}

# Step 1: apply all, snapshot.
echo "==> first up"
run_migrate up
SNAP_A="$(mktemp)"
pg_dump_schema "$SNAP_A"

# Step 2: roll back fully.
echo "==> rollback all"
while :; do
  remaining=$(psql "$DB_URL" -tA -c "SELECT count(*) FROM schema_migrations")
  if [ "$remaining" = "0" ]; then break; fi
  run_migrate down
done

# After full rollback, only the schema_migrations table itself should exist
# (the migrate.sh runner created it, so we don't expect it gone). Any
# domain-table residue is a leak.
leftovers=$(psql "$DB_URL" -tA <<'SQL'
SELECT table_name FROM information_schema.tables
WHERE table_schema='public' AND table_name <> 'schema_migrations'
ORDER BY table_name;
SQL
)
if [ -n "$leftovers" ]; then
  echo "down migrations left tables behind:" >&2
  echo "$leftovers" >&2
  exit 1
fi

# Step 3: re-apply, snapshot.
echo "==> second up"
run_migrate up
SNAP_B="$(mktemp)"
pg_dump_schema "$SNAP_B"

# Step 4: schemas must match byte-for-byte.
if ! diff -u "$SNAP_A" "$SNAP_B"; then
  echo "schema drift between first and second up" >&2
  exit 1
fi

echo "==> roundtrip ok"
rm -f "$SNAP_A" "$SNAP_B"
