#!/usr/bin/env bash
# Minimal forward-only migration runner using psql.
# For production use golang-migrate / atlas — schema is compatible.
set -euo pipefail

DIR="$(cd "$(dirname "$0")/.." && pwd)"
MIG="$DIR/db/migrations"
DB_URL="${DATABASE_URL:-postgres://naditos:naditos@localhost:5432/naditos?sslmode=disable}"

cmd="${1:-up}"

ensure_table() {
  psql "$DB_URL" -v ON_ERROR_STOP=1 -q <<'SQL' >/dev/null
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
SQL
}

case "$cmd" in
  up)
    ensure_table
    for f in "$MIG"/*.up.sql; do
      v="$(basename "$f" .up.sql)"
      already=$(psql "$DB_URL" -tA -c "SELECT 1 FROM schema_migrations WHERE version='$v'")
      if [ "$already" = "1" ]; then
        echo "skip   $v"
        continue
      fi
      echo "apply  $v"
      psql "$DB_URL" -v ON_ERROR_STOP=1 -q -f "$f"
      psql "$DB_URL" -q -c "INSERT INTO schema_migrations(version) VALUES ('$v')"
    done
    ;;
  down)
    last=$(psql "$DB_URL" -tA -c "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1")
    if [ -z "$last" ]; then echo "nothing to roll back"; exit 0; fi
    f="$MIG/$last.down.sql"
    echo "rollback $last"
    psql "$DB_URL" -v ON_ERROR_STOP=1 -q -f "$f"
    psql "$DB_URL" -q -c "DELETE FROM schema_migrations WHERE version='$last'"
    ;;
  *) echo "usage: $0 up|down"; exit 2;;
esac
