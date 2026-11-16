#!/usr/bin/env bash
# psql-based migration runner for machines without Docker or golang-migrate. It keeps the same
# `schema_migrations (version, dirty)` table as golang-migrate, so the two tools are interchangeable
# (and db/ops/restore_smoke.sql works).  Run it as the redxtube_migrator role (NOT a superuser: the API role's
# privileges come from default privileges that only apply to objects the migrator creates):
#   DATABASE_URL=postgres://redxtube_migrator:...@host/redxtube db/scripts/migrate.sh up | down [N] | status
set -euo pipefail
: "${DATABASE_URL:?DATABASE_URL is required (owner or superuser DSN of the target database)}"
DIR="$(cd "$(dirname "$0")/../migrations" && pwd)"
q() { psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qtA "$@"; }

q -c "CREATE TABLE IF NOT EXISTS schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL)"
current() { q -c "SELECT COALESCE(max(version), 0) FROM schema_migrations"; }
ver() { echo $((10#$(basename "$1" | cut -d_ -f1))); }
set_version() { q -c "TRUNCATE schema_migrations" -c "INSERT INTO schema_migrations VALUES ($1, false)"; }

case "${1:-}" in
  up)
    cur=$(current)
    for f in "$DIR"/*.up.sql; do
      v=$(ver "$f")
      [ "$v" -le "$cur" ] && continue
      echo "applying $(basename "$f")"
      psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q --single-transaction -f "$f" >/dev/null
      set_version "$v"
    done ;;
  down)
    steps="${2:-1}"
    for _ in $(seq "$steps"); do
      cur=$(current)
      if [ "$cur" -eq 0 ]; then break; fi
      f=""; prev=0
      for x in "$DIR"/*.up.sql; do
        v=$(ver "$x")
        if [ "$v" -eq "$cur" ]; then f="${x%.up.sql}.down.sql"; fi
        if [ "$v" -lt "$cur" ] && [ "$v" -gt "$prev" ]; then prev="$v"; fi
      done
      [ -f "$f" ] || { echo "no down migration for version $cur" >&2; exit 1; }
      echo "reverting $(basename "$f")"
      psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q --single-transaction -f "$f" >/dev/null
      set_version "$prev"
    done ;;
  status) echo "version: $(current)" ;;
  *) echo "usage: $0 up | down [N] | status" >&2; exit 2 ;;
esac
