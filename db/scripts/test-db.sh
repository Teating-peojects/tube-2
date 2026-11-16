#!/usr/bin/env bash
# Builds the databases the integration tests need, without Docker:
#   roles (db/bootstrap/roles.sql) -> migrations -> reference data (categories).
#   ADMIN_URL=postgres://postgres:secret@127.0.0.1:5432/postgres db/scripts/test-db.sh [dbname]
# Builds TWO databases and prints the TEST_* variables to export (stdout is `export ...` lines only; CI sources it):
#   <dbname>            API/handler tests (they TRUNCATE tables between tests)
#   <dbname minus _test>_worker_test   worker tests: `go test ./...` runs packages in parallel, so a package
#                       that shares a database with the handler tests would have its rows wiped mid-test.
# DESTROYS and recreates both (the name must end in _test).
set -euo pipefail
: "${ADMIN_URL:?ADMIN_URL is required (superuser DSN, any database)}"
DB="${1:-redxtube_test}"
case "$DB" in *_test) ;; *) echo "refusing: database name must end in _test" >&2; exit 2 ;; esac
WDB="${DB%_test}_worker_test"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
base="${ADMIN_URL%/*}"                      # postgres://user:pass@host:port
app_pw="${APP_PASSWORD:-app_dev}"
mig_pw="${MIGRATOR_PASSWORD:-mig_dev}"
wrk_pw="${WORKER_PASSWORD:-wrk_dev}"
host="${base#*@}"

build() {
  local name="$1"
  psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -q -c "DROP DATABASE IF EXISTS $name" -c "CREATE DATABASE $name"
  # roles are cluster-wide and created only if missing; the grants are per database
  psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -q -v dbname="$name" -f "$ROOT/bootstrap/roles.sql" >/dev/null 2>&1 || true
  psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -q -c "ALTER ROLE redxtube_app PASSWORD '$app_pw'" -c "ALTER ROLE redxtube_migrator PASSWORD '$mig_pw'" -c "ALTER ROLE redxtube_worker PASSWORD '$wrk_pw'"

  # IMPORTANT: migrations must run as redxtube_migrator, not as a superuser. roles.sql grants the API
  # role its privileges through ALTER DEFAULT PRIVILEGES FOR ROLE redxtube_migrator, so tables created
  # by any other role are invisible to redxtube_app ("permission denied for table users").
  local mig="postgres://redxtube_migrator:$mig_pw@$host/$name"
  # stdout of this script is sourced by CI (it must contain only `export ...` lines): progress goes to stderr
  DATABASE_URL="$mig" "$ROOT/scripts/migrate.sh" up >&2
  psql "$mig" -v ON_ERROR_STOP=1 -q -f "$ROOT/seeds/reference_data.sql"
}

build "$DB"
build "$WDB"

echo "export TEST_ADMIN_DATABASE_URL='$base/$DB'"
echo "export TEST_DATABASE_URL='postgres://redxtube_app:$app_pw@$host/$DB'"
echo "export TEST_WORKER_ADMIN_DATABASE_URL='$base/$WDB'"
echo "export TEST_WORKER_DATABASE_URL='postgres://redxtube_worker:$wrk_pw@$host/$WDB'"
echo "export TEST_WORKER_APP_DATABASE_URL='postgres://redxtube_app:$app_pw@$host/$WDB'"
echo "export TEST_REDIS_URL='redis://127.0.0.1:6379/0'"
