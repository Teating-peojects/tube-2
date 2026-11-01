#!/usr/bin/env bash
# Builds the database the integration tests need, without Docker:
#   roles (db/bootstrap/roles.sql) -> migrations -> reference data (categories).
#   ADMIN_URL=postgres://postgres:secret@127.0.0.1:5432/postgres db/scripts/test-db.sh [dbname]
# Prints the three TEST_* variables to export. DESTROYS and recreates the database (name must end in _test).
set -euo pipefail
: "${ADMIN_URL:?ADMIN_URL is required (superuser DSN, any database)}"
DB="${1:-redxtube_test}"
case "$DB" in *_test) ;; *) echo "refusing: database name must end in _test" >&2; exit 2 ;; esac
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
base="${ADMIN_URL%/*}"                      # postgres://user:pass@host:port
app_pw="${APP_PASSWORD:-app_dev}"
mig_pw="${MIGRATOR_PASSWORD:-mig_dev}"

psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -q -c "DROP DATABASE IF EXISTS $DB" -c "CREATE DATABASE $DB"
psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -q -v dbname="$DB" -f "$ROOT/bootstrap/roles.sql" >/dev/null 2>&1 || true
psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -q -c "ALTER ROLE redxtube_app PASSWORD '$app_pw'" -c "ALTER ROLE redxtube_migrator PASSWORD '$mig_pw'"

# IMPORTANT: migrations must run as redxtube_migrator, not as a superuser. roles.sql grants the API
# role its privileges through ALTER DEFAULT PRIVILEGES FOR ROLE redxtube_migrator, so tables created
# by any other role are invisible to redxtube_app ("permission denied for table users").
host="${base#*@}"
mig="postgres://redxtube_migrator:$mig_pw@$host/$DB"
DATABASE_URL="$mig" "$ROOT/scripts/migrate.sh" up
psql "$mig" -v ON_ERROR_STOP=1 -q -f "$ROOT/seeds/reference_data.sql"

echo "export TEST_ADMIN_DATABASE_URL='$base/$DB'"
echo "export TEST_DATABASE_URL='postgres://redxtube_app:$app_pw@$host/$DB'"
echo "export TEST_REDIS_URL='redis://127.0.0.1:6379/0'"
