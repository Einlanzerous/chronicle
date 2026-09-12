#!/usr/bin/env bash
# Chronicle: database, roles, and the tier lockdown, for a Postgres that
# construct-server does NOT provision. Run as a Postgres superuser.
#
# ON THE ESTATE THIS IS NOT THE PATH. Since SERV-169 and SERV-182,
# construct-server/db/init-db.sh creates the `chronicle` role, both databases
# and — in a dedicated block that never goes through its ensure_db helper —
# the `chronicle_tier1` role with CONNECT and nothing else, on every deploy.
# This script is for a developer's own Postgres, or for a rebuild somewhere
# that script does not run. Both paths apply the same lockdown, because the
# tier-1 half of it lives in ONE file, deploy/tier1-role.sql, which CI applies
# to chronicle_test as well: see that file's header for who consumes it.
#
# Passwords are never written here or into a compose file — they live in Signet
# and are injected for the length of this command:
#
#   signet exec --secret construct-server/CHRONICLE_DB_PASSWORD \
#               --secret construct-server/CHRONICLE_TIER1_DB_PASSWORD \
#               -- deploy/provision-db.sh
#
# Idempotent: re-running resets both role passwords to the current Signet
# values and re-asserts the grants, which is also how a rotation is applied.
#
# After a provisioning change, `chronicle tier1-audit` against the database is
# the check that the role still holds exactly what the tier boundary allows;
# `chronicle serve` runs the same audit at boot and refuses to start otherwise.
set -euo pipefail

: "${CHRONICLE_DB_PASSWORD:?not set — run under: signet exec --secret construct-server/CHRONICLE_DB_PASSWORD ...}"
: "${CHRONICLE_TIER1_DB_PASSWORD:?not set — run under: signet exec --secret construct-server/CHRONICLE_TIER1_DB_PASSWORD ...}"

cd "$(dirname "${BASH_SOURCE[0]}")"

PG_CONTAINER="${PG_CONTAINER:-postgres}"
DB="${CHRONICLE_DB_NAME:-chronicle}"

psql_super() {
  docker exec -i \
    -e APP_PW="$CHRONICLE_DB_PASSWORD" \
    -e T1_PW="$CHRONICLE_TIER1_DB_PASSWORD" \
    -e DB="$DB" \
    "$PG_CONTAINER" psql -U postgres -v ON_ERROR_STOP=1 -q "$@"
}

# --- the application role and the database itself (cluster scope) ----------
psql_super -d postgres <<'SQL'
\getenv app_pw APP_PW
\getenv db     DB

-- chronicle: the application role. Owns everything inside its own database.
-- \gset + \if, not \gexec: \gexec echoes the generated statement, password
-- included, to the terminal this runs in.
SELECT NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'chronicle') AS app_missing \gset
\if :app_missing
CREATE ROLE chronicle LOGIN PASSWORD :'app_pw';
\endif
ALTER ROLE chronicle LOGIN PASSWORD :'app_pw';

SELECT NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'db') AS db_missing \gset
\if :db_missing
CREATE DATABASE :"db" OWNER chronicle;
\endif
SQL

# --- the regeneration role, and the lockdown it relies on --------------------
# Connected to the database, because the schema revoke is per database. This is
# the shared definition; do not restate it here.
psql_super -d "$DB" < tier1-role.sql

# --- what the application role gets back after the lockdown ------------------
psql_super -d "$DB" <<'SQL'
\getenv db DB

-- Its own database is the doctrine's outer boundary: nobody reaches it who was
-- not named. tier1-role.sql revoked PUBLIC; chronicle is named here.
GRANT CONNECT ON DATABASE :"db" TO chronicle;

ALTER SCHEMA public OWNER TO chronicle;
GRANT ALL ON SCHEMA public TO chronicle;
SQL

echo "provisioned: database ${DB}, roles chronicle + chronicle_tier1"
