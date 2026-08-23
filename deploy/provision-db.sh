#!/usr/bin/env bash
# Chronicle: database, roles, and the tier lockdown. Run once, as a Postgres
# superuser, against the shared Postgres 16 on construct_net.
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
set -euo pipefail

: "${CHRONICLE_DB_PASSWORD:?not set — run under: signet exec --secret construct-server/CHRONICLE_DB_PASSWORD ...}"
: "${CHRONICLE_TIER1_DB_PASSWORD:?not set — run under: signet exec --secret construct-server/CHRONICLE_TIER1_DB_PASSWORD ...}"

PG_CONTAINER="${PG_CONTAINER:-postgres}"
DB="${CHRONICLE_DB_NAME:-chronicle}"

psql_super() {
  docker exec -i \
    -e APP_PW="$CHRONICLE_DB_PASSWORD" \
    -e T1_PW="$CHRONICLE_TIER1_DB_PASSWORD" \
    -e DB="$DB" \
    "$PG_CONTAINER" psql -U postgres -v ON_ERROR_STOP=1 -q "$@"
}

# --- roles and the database itself (cluster scope) ------------------------
psql_super -d postgres <<'SQL'
\getenv app_pw APP_PW
\getenv t1_pw  T1_PW
\getenv db     DB

-- chronicle: the application role. Owns everything inside its own database.
SELECT format('CREATE ROLE chronicle LOGIN PASSWORD %L', :'app_pw')
 WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'chronicle');
\gexec
ALTER ROLE chronicle LOGIN PASSWORD :'app_pw';

-- chronicle_tier1: the regeneration role. Tier 1 only, by construction.
SELECT format('CREATE ROLE chronicle_tier1 LOGIN PASSWORD %L', :'t1_pw')
 WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'chronicle_tier1');
\gexec
ALTER ROLE chronicle_tier1 LOGIN PASSWORD :'t1_pw';

SELECT format('CREATE DATABASE %I OWNER chronicle', :'db')
 WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'db');
\gexec
SQL

# --- lockdown inside Chronicle's own database -----------------------------
psql_super -d "$DB" <<'SQL'
\getenv db DB

-- Its own database is the doctrine's outer boundary: nobody reaches it who was
-- not named. Postgres grants CONNECT to PUBLIC by default; that is revoked.
REVOKE ALL ON DATABASE :"db" FROM PUBLIC;
GRANT CONNECT ON DATABASE :"db" TO chronicle, chronicle_tier1;

ALTER SCHEMA public OWNER TO chronicle;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT ALL ON SCHEMA public TO chronicle;
SQL

echo "provisioned: database ${DB}, roles chronicle + chronicle_tier1"
